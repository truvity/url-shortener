#!/usr/bin/env python3
"""Compare the schemas the Pkl contract generates with the hand-written ones.

SHADOW PHASE (docs/guides/pkl-shadow.md). The hand-written schemas are
authoritative; this reports where the generated ones differ, so that the
switch-or-stop decision of decision 0010 is made on a list rather than on an
impression. It is REPORT-ONLY: a difference is a line in the report, never a
failure. It fails only when it cannot do its job (a file it needs is missing).

    hack/shadow-diff.py GENERATED_DIR [--report FILE] [--summary FILE]

GENERATED_DIR is what hack/shadow-generate.sh wrote. The Justfile recipe
(`just shadow-diff`) generates fresh and runs this with the project's Python,
which has `jsonschema` and `pyyaml`.

What is compared, and how:

  documents  every hand-written config schema and fragment against the
             generated document with the same file name;
  charts     every chart's values.schema.json against the generated one;
  defaults   what each chart's values.yaml sets, against the default the
             generated schema carries at that key (a hand-written chart keeps
             its defaults in values.yaml, not in its schema), and the one
             generated values.yaml against its hand-written twin;
  verdicts   the existing fixtures, run through both schemas of each pair.

Schemas are compared SEMANTICALLY: every `$ref` is resolved, so a shared
sub-schema inlined on one side and referenced on the other is not a
difference; `allOf` of objects is merged; `unevaluatedProperties: false` over
a merged base equals `additionalProperties: false`; a pattern's companion
`not: {pattern: ...}` over the line breaks is a flag, not a second pattern; ordering is ignored.

Each difference has a category (what differs), a class and a cause:

  structural  a by-design consequence of generating from one source, gone at
            the switch (the generated schemas state defaults, the hand-written
            ones keep them in values.yaml); neither a rule nor a finding;
  expected  a semantic rule decision 0010 states (a field with a default is
            optional, `null` is refused, a pattern refuses a line break, its `\\s` and `.` are spelled as classes);
  gap       a real difference: the contract does not say what the hand-written
            schema says (a missing constraint the vocabulary cannot spell, a
            field not modelled, ...), to be fixed in the contract or in the
            vocabulary, or accepted into the hand-written schema at the switch;
  doc       description text only: documentation, not validation.
"""

from __future__ import annotations

import argparse
import copy
import json
import os
import re
import sys
from collections import Counter
from pathlib import Path
from urllib.parse import urljoin

import yaml

ROOT = Path(__file__).resolve().parent.parent
EX = ROOT
CHARTS = EX / "charts"
# truvity/policy owns the shared schemas (the envelope and the fragments):
# the checkout of the module version go.mod pins, which the Justfile hands over.
POLICY = Path(os.environ["POLICY_DIR"])

MISSING = object()

# Keywords the normaliser understands. Anything else is carried as `other` and
# compared as it stands, so that a keyword added by a generator is a finding.
KNOWN = {
    "$schema", "$id", "$ref", "$defs", "$comment", "title", "description",
    "type", "enum", "const", "properties", "required", "additionalProperties",
    "unevaluatedProperties", "items", "minimum", "maximum", "exclusiveMinimum",
    "exclusiveMaximum", "minLength", "maxLength", "pattern", "format", "default",
    "allOf", "anyOf", "if", "then", "else", "not", "propertyNames",
}  # fmt: skip

# Keywords no validator reads: carried as a flag of their own, never as a
# keyword only one side has. `x-set-at-install` is pkl-contracts' mark for a
# value only an install can give (v0.3.1), see the `set-at-install-marker` category.
ANNOTATIONS = {"x-set-at-install"}

# ----------------------------------------------------------------- documents

# (name used in the report, hand-written file, generated file relative to the
# generated directory). The fragments and the envelope are the policy's own
# (schemas/); the rest are this example's.
CONFIGS = [
    ("migrate", EX / "schemas" / "migrate.json", "schemas/migrate.json"),
    ("prober", EX / "schemas" / "prober.json", "schemas/prober.json"),
    ("redirect", EX / "schemas" / "redirect.json", "schemas/redirect.json"),
    ("stat", EX / "schemas" / "stat.json", "schemas/stat.json"),
    ("urls", EX / "schemas" / "urls.json", "schemas/urls.json"),
    ("web", EX / "schemas" / "web.json", "schemas/web.json"),
    ("log", EX / "log" / "src" / "url_shortener_log" / "log.schema.json", "schemas/log.json"),
    ("echo", CHARTS / "testdata" / "service-example" / "echo.schema.json", "schemas/echo.json"),
    ("service", POLICY / "schemas" / "service.json", "schemas/service.json"),
]  # fmt: skip
# Fragments the contract package does not model yet, which may lack a generated
# twin (a `document-missing` gap in the report). Anything else missing fails.
NO_PKL_TWIN = {"fragments/secrets"}
FRAGMENTS = sorted((POLICY / "schemas" / "fragments").glob("*.json"))

# chart name -> (hand-written values.schema.json, its values.yaml)
CHART_FILES = {
    "url-shortener": (CHARTS / "url-shortener" / "values.schema.json", CHARTS / "url-shortener" / "values.yaml"),
    "url-shortener-infra": (CHARTS / "url-shortener-infra" / "values.schema.json", CHARTS / "url-shortener-infra" / "values.yaml"),
    "service-example": (CHARTS / "testdata" / "service-example" / "values.schema.json", CHARTS / "testdata" / "service-example" / "values.yaml"),
}  # fmt: skip

# fixtures: chart name -> files laid over the chart's values.yaml (as Helm does)
TD = CHARTS / "testdata"
CHART_FIXTURES = {
    "url-shortener": [TD / "minimal.yaml", TD / "everything.yaml", TD / "per-component.yaml", *sorted((TD / "invalid").glob("*.yaml"))],
    "url-shortener-infra": [TD / "infra-minimal.yaml", TD / "infra-everything.yaml", TD / "infra-platform-owned.yaml"],
    "service-example": [TD / "service-example-everything.yaml"],
}  # fmt: skip
CONFIG_FIXTURES = {  # config name -> example configuration files
    name: [EX / "internal" / "config" / "testdata" / f"{name}.yaml"]
    for name in ("migrate", "prober", "redirect", "stat", "urls")
}  # fmt: skip


def rel(p: Path) -> str:
    try:
        return str(p.relative_to(ROOT))
    except ValueError:
        return str(p)


def load_json(p: Path):
    with open(p, encoding="utf-8") as f:
        return json.load(f)


def load_yaml(p: Path):
    with open(p, encoding="utf-8") as f:
        return yaml.safe_load(f)


class Registry:
    """Documents by `$id`, including those embedded under a `$defs`."""

    def __init__(self):
        self.by_id: dict[str, dict] = {}

    def add(self, doc: dict):
        def crawl(node):
            if isinstance(node, dict):
                if isinstance(node.get("$id"), str):
                    self.by_id[node["$id"]] = node
                for v in node.values():
                    crawl(v)
            elif isinstance(node, list):
                for v in node:
                    crawl(v)

        crawl(doc)


# ------------------------------------------------------------- normalisation


def squash(s):
    return re.sub(r"\s+", " ", s).strip() if isinstance(s, str) else s


def infer_types(vals):
    out = set()
    for v in vals:
        out.add(
            "string" if isinstance(v, str) else "boolean" if isinstance(v, bool)
            else "integer" if isinstance(v, int) else "number" if isinstance(v, float)
            else "null" if v is None else "other"
        )  # fmt: skip
    return sorted(out)


class Normaliser:
    def __init__(self, registry: Registry, opaque: bool = True):
        self.reg = registry
        # A `$ref` to another DOCUMENT is kept as a reference (and the documents
        # are compared on their own), so that a difference inside a fragment is
        # one line, not one line for every schema that uses it. A pointer into a
        # document, or a local `#/$defs/...`, is always followed.
        self.opaque = opaque

    def expand(self, n: dict) -> dict:
        """A node that is only a reference, followed."""
        if "ref" not in n:
            return n
        doc = self.reg.by_id[n["ref"]]
        full = Normaliser(self.reg, opaque=False).norm(doc, n["ref"], doc)
        full = copy.deepcopy(full)
        for k in ("description", "title"):
            if k in n:
                full[k] = n[k]
        return full

    def resolve(self, ref: str, base: str, root: dict):
        if ref.startswith("#/$defs/"):
            return root["$defs"][ref[len("#/$defs/"):]], base, root
        target, _, pointer = urljoin(base, ref).partition("#")
        if target not in self.reg.by_id:
            raise KeyError(f"unresolved $ref {ref!r} (from {base!r})")
        doc = self.reg.by_id[target]
        node = doc
        for part in [x for x in pointer.split("/") if x]:
            node = node[part.replace("~1", "/").replace("~0", "~")]
        return node, target, doc

    def norm(self, s, base: str, root: dict) -> dict:
        """The normal form of schema `s`: a dict of what it says, ordering gone."""
        if s is True or s == {}:
            return {}
        if s is False:
            return {"never": True}
        if "$id" in s:
            base = s["$id"]
            root = s
        n: dict = {}
        if "$ref" in s and self.opaque and "#" not in s["$ref"] and urljoin(base, s["$ref"]) in self.reg.by_id:
            n = {"ref": urljoin(base, s["$ref"])}
        elif "$ref" in s:
            target, tbase, troot = self.resolve(s["$ref"], base, root)
            n = self.norm(target, tbase, troot)
            n = copy.deepcopy(n)
            n.pop("title", None) if "title" not in s else None
        # siblings of $ref (a description beside it) win over the target's
        for k in ("description", "title"):
            if k in s:
                n[k] = squash(s[k])
        if "type" in s:
            t = s["type"]
            n["types"] = sorted(t if isinstance(t, list) else [t])
        if "enum" in s:
            n["enum"] = sorted(s["enum"], key=lambda x: json.dumps(x))
            if "types" not in n:
                n["types"] = infer_types(s["enum"])
        if "const" in s:
            n["enum"] = [s["const"]]
            n.setdefault("types", infer_types([s["const"]]))
        for k in ("minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "minLength", "maxLength", "format"):
            if k in s:
                n[k] = s[k]
        if "pattern" in s:
            n["pattern"] = s["pattern"]
        if "not" in s and is_line_break_guard(s["not"]):
            n["noNewline"] = True
        elif "not" in s:
            n.setdefault("other", {})["not"] = s["not"]
        if "default" in s:
            n["default"] = s["default"]
        if "properties" in s or "required" in s:
            n.setdefault("types", ["object"])
        if "properties" in s:
            props = dict(n.get("props", {}))
            for k, v in s["properties"].items():
                props[k] = self.norm(v, base, root)
            n["props"] = props
        if "required" in s:
            n["required"] = sorted(set(n.get("required", [])) | set(s["required"]))
        if s.get("additionalProperties") is False or s.get("unevaluatedProperties") is False:
            n["closed"] = True
        elif isinstance(s.get("additionalProperties"), dict):
            n["additional"] = self.norm(s["additionalProperties"], base, root)
        if "propertyNames" in s:
            n["propertyNames"] = self.norm(s["propertyNames"], base, root)
        if "items" in s:
            n["items"] = self.norm(s["items"], base, root)
            n.setdefault("types", ["array"])
        if "anyOf" in s:
            n["anyOf"] = sorted(
                (self.norm(m, base, root) for m in s["anyOf"]),
                key=lambda x: json.dumps(x, sort_keys=True),
            )
        conds = list(n.get("conditionals", []))
        if "if" in s:
            cond = {"if": self.norm(s["if"], base, root), "then": self.norm(s.get("then", {}), base, root)}
            if "else" in s:
                # `@RequiredUnless` is an `if`/`else`; a rule that only differs in its `else` is a difference
                cond["else"] = self.norm(s["else"], base, root)
            conds.append(cond)
        for member in s.get("allOf", []):
            m = self.norm(member, base, root)
            conds.extend(m.pop("conditionals", []))
            merge_into(n, m)
        if conds:
            n["conditionals"] = sorted(conds, key=lambda x: json.dumps(x, sort_keys=True))
        if s.get("x-set-at-install") is True:
            n["setAtInstall"] = True
        extra = {k: v for k, v in s.items() if k not in KNOWN and k not in ANNOTATIONS}
        if extra:
            n.setdefault("other", {}).update(extra)
        return n


def merge_into(n: dict, m: dict):
    """Fold the normal form of an `allOf` member into its parent's."""
    for k, v in m.items():
        if k == "props":
            n.setdefault("props", {}).update(v)
        elif k == "required":
            n["required"] = sorted(set(n.get("required", [])) | set(v))
        elif k == "ref":
            n["bases"] = sorted(set(n.get("bases", [])) | {v})
        elif k == "closed":
            pass  # a member's own closure does not close the parent: allOf adds, it does not narrow keys
        elif k in ("title", "description"):
            pass  # the parent's own
        elif k == "types":
            n.setdefault("types", v)
        else:
            n.setdefault(k, v)


# ---------------------------------------------------------------------- diff


class Diff:
    def __init__(self, doc, path, category, klass, hand, gen, cause):
        self.doc, self.path, self.category, self.klass = doc, path, category, klass
        self.hand, self.gen, self.cause = hand, gen, cause

    def key(self):
        return (self.doc, self.path, self.category)


# What pkl-contracts spells where a hand-written pattern says `\s` or `.`
# (v0.3.0: an explicit class, the same in every engine).
WHITE_SPACE_CLASS = "\\t \\xA0\u1680\u2000-\u200A\u202F\u205F\u3000"


def is_line_break_guard(n) -> bool:
    """The companion `not: {pattern: ...}` of a pattern: `\\n` (v0.2) or a class of the seven line breaks (v0.3)."""
    if not (isinstance(n, dict) and set(n) == {"pattern"}):
        return False
    p = n["pattern"]
    return p == "\\n" or (p.startswith("[") and p.endswith("]") and "\\n" in p and "\\r" in p)


def empty_form_kept(hand: str, gen: str) -> bool:
    """The hand-written pattern is the generated one with the empty string also allowed: `^(X)?$` against `^X$`."""
    return gen.startswith("^") and gen.endswith("$") and hand == f"^({gen[1:-1]})?$"


def respelled(hand: str, gen: str) -> bool:
    """The generated pattern is the hand-written one with `\\s` and `.` spelled as classes."""
    return gen.replace(WHITE_SPACE_CLASS, "\\s").replace("[^\\n]", ".") == hand


CAUSES = {
    "empty-form-kept": "the hand-written schema keeps the empty string as a value meaning \"none\" so that a published chart's `\"\"` keeps rendering (taking it out is a major release); the contract has the field absent instead (an optional field is absent or non-empty). Gone at the switch, which is that release",
    "pattern-spelling": "pkl-contracts v0.3.0 spells white space and `.` as explicit classes, the same in every engine; the hand-written `\\s` and `.` are read differently by each",
    "set-at-install": "pkl-contracts v0.3.0 `@A.SetAtInstall`: the schema requires a real value and the defaults leave it out; the hand-written values.yaml ships an empty placeholder",
    "newline-guard": "decision 0010: a pattern refuses every line break; the generated schema adds `not: {pattern: \"[...]\"}` over them",
    "set-at-install-marker": "pkl-contracts v0.3.1 marks a value only an install can give `x-set-at-install: true` (an annotation no validator reads); the hand-written schema marks nothing, it only requires the key",
    "required-to-default": "decision 0010: a field with a default is optional (the generated property carries the `default`, a block's own default when every field of it has one)",
    "required-to-optional": "the contract declares the field optional with no default",
    "optional-to-required": "the contract declares the field required (the hand-written schema leaves it optional)",
    "null-refused": "decision 0010: `null` is not a value for an optional field",
    "type": "the contract's type for the field is not the hand-written one",
    "property-missing": "the contract does not model this property",
    "property-extra": "the contract models a property the hand-written schema does not have",
    "pattern-missing": "the contract uses a type with no pattern here (the vocabulary has no alias for this pattern, or a plain type was used)",
    "pattern-added": "the contract's type carries a pattern the hand-written schema does not",
    "pattern": "the contract's pattern is not the hand-written one (the nearest vocabulary alias differs)",
    "range-missing": "the vocabulary has no alias with this bound, or a plain type was used",
    "range": "the contract's bound is not the hand-written one (the nearest vocabulary alias differs)",
    "length-missing": "the vocabulary has no alias with this length bound",
    "length": "the contract's length bound is not the hand-written one",
    "length-added": "the contract's type bounds the length (a DNS label is at most 63 characters, RFC 1123) where the hand-written pattern leaves it open; nothing a cluster accepts is longer, so the switch refuses nothing real",
    "enum": "the contract's allowed values are not the hand-written ones",
    "default-missing": "the generated schema carries no `default` where the hand-written one does",
    "default-added": "the contract states a default the hand-written schema does not",
    "default": "the contract's default is not the hand-written one",
    "closed": "the contract and the hand-written schema disagree on whether an undeclared key is refused",
    "union": "the contract's alternatives are not the hand-written ones",
    "conditional": "the cross-field rule differs",
    "conditional-blocks": "the same rule (the same `if`, the same values required in its `else`); it differs only in the blocks on the way that it also names. The generated rule names the blocks it walks through (`database.owner`, `archive.bucket`), the hand-written one names the top-level blocks that carry a default (`log`, `replicas`, `resources`, `route`, `images`, now optional with their default); a block is in every value set once `values.yaml`'s defaults are merged, so no render is accepted or refused differently",
    "keyword": "a keyword only one side carries",
    "format": "the contract's `format` is not the hand-written one",
    "title": "the document's title differs",
    "description": "documentation text differs (wording only; not validated)",
    "description-added": "the contract documents a field the hand-written schema leaves undocumented",
    "document-missing": "no generated document with this name",
    "document-extra": "a generated document with no hand-written twin",
}  # fmt: skip

# By-design consequences of generating from one source, gone at the switch:
# the hand-written schemas keep no defaults (values.yaml has them), the
# generated ones do.
STRUCTURAL = {"default-added"}
EXPECTED = {"set-at-install", "set-at-install-marker", "pattern-spelling", "newline-guard", "required-to-default", "null-refused", "empty-form-kept", "conditional-blocks"}


def required_paths(node, prefix="") -> set[str]:
    """Every dotted path a normalised schema requires, blocks on the way included."""
    out: set[str] = set()
    for name in node.get("required", []):
        out.add(prefix + name)
    for name, sub in node.get("props", {}).items():
        out |= required_paths(sub, f"{prefix}{name}.")
    return out


def node_at(root: dict, path: str):
    node = root
    for part in path.split("."):
        node = node.get("props", {}).get(part)
        if node is None:
            return None
    return node


def values_of(paths: set[str], root: dict) -> set[str]:
    """The paths that are values: not a block (an object with properties of its own)
    in the document, and not a prefix of another path."""
    out = set()
    for p in paths:
        if any(q.startswith(p + ".") for q in paths):
            continue
        node = node_at(root, p)
        if node is not None and node.get("props"):
            continue
        out.add(p)
    return out


def same_leaves(hc, gc, h_root, g_root) -> bool:
    """Two rules that carry the same `if`/`then` and require the same VALUES in their
    `else`, and differ only in which blocks (objects of the document's own) they also name."""
    if not hc or not gc or len(hc) != 1 or len(gc) != 1:
        return False
    h, g = hc[0], gc[0]
    if not jeq(h.get("if"), g.get("if")) or not jeq(h.get("then"), g.get("then")) or "else" not in h or "else" not in g:
        return False
    hv = values_of(required_paths(h["else"]), h_root)
    gv = values_of(required_paths(g["else"]), g_root)
    return hv == gv and bool(gv)


def short(v):
    s = json.dumps(v, ensure_ascii=False, sort_keys=True) if not isinstance(v, str) else v
    s = s.replace("|", "\\|").replace("\n", " ")
    return s if len(s) <= 90 else s[:87] + "..."


def jeq(a, b):
    return json.dumps(a, sort_keys=True) == json.dumps(b, sort_keys=True)


def compare(doc: str, h: dict, g: dict, path: str, out: list[Diff], has_default=None, expand=None):
    def add(cat, hand, gen, klass=None):
        out.append(Diff(doc, path or "(document)", cat, klass or ("expected" if cat in EXPECTED else "structural" if cat in STRUCTURAL else "doc" if cat in ("description", "description-added", "title") else "gap"), hand, gen, CAUSES[cat]))  # fmt: skip

    if "ref" in h or "ref" in g:
        if "ref" in h and "ref" in g:
            # the same document on both sides: it is compared as a document
            if h["ref"] != g["ref"]:
                add("keyword", h["ref"], g["ref"])
            if h.get("description") != g.get("description"):
                add("description" if h.get("description") else "description-added", h.get("description"), g.get("description"))
            return
        h, g = expand[0](h), expand[1](g)  # one side refers, the other inlines
    if h.get("bases") != g.get("bases"):
        add("keyword", f"allOf {h.get('bases')}", f"allOf {g.get('bases')}")
    ht, gt = h.get("types"), g.get("types")
    if ht != gt:
        if ht and gt and "null" in ht and "null" not in gt and [t for t in ht if t != "null"] == gt:
            add("null-refused", ht, gt)
        else:
            add("type", ht, gt)
    for k, cat in (("minimum", "range"), ("maximum", "range"), ("exclusiveMinimum", "range"), ("exclusiveMaximum", "range")):
        if h.get(k, MISSING) != g.get(k, MISSING) and not (k in h and k in g and h[k] == g[k]):
            hv, gv = h.get(k), g.get(k)
            add("range-missing" if gv is None else "range", f"{k} {hv}", f"{k} {gv}")
    for k in ("minLength", "maxLength"):
        if h.get(k) != g.get(k):
            hv, gv = h.get(k), g.get(k)
            add("length-missing" if gv is None else "length-added" if hv is None else "length", f"{k} {hv}", f"{k} {gv}")
    hp, gp = h.get("pattern"), g.get("pattern")
    if hp != gp:
        if hp is not None and gp is not None and respelled(hp, gp):
            add("pattern-spelling", hp, gp)
        elif hp is not None and gp is not None and empty_form_kept(hp, gp):
            add("empty-form-kept", hp, gp)
        else:
            add("pattern-missing" if gp is None else "pattern-added" if hp is None else "pattern", hp, gp)
    if gp is not None and g.get("noNewline") and not h.get("noNewline"):
        add("newline-guard", hp, gp + "  +  not line break")
    if h.get("format") != g.get("format"):
        add("format", h.get("format"), g.get("format"))
    if h.get("enum") != g.get("enum"):
        add("enum", h.get("enum"), g.get("enum"))
    if ("default" in h) != ("default" in g):
        if "default" in h:
            add("default-missing", h["default"], None)
        else:
            add("default-added", None, g["default"])
    elif "default" in h and not jeq(h["default"], g["default"]):
        add("default", h["default"], g["default"])
    if bool(h.get("closed")) != bool(g.get("closed")):
        add("closed", bool(h.get("closed")), bool(g.get("closed")))
    if h.get("description") != g.get("description"):
        add("description" if h.get("description") else "description-added", h.get("description"), g.get("description"))
    if h.get("title") != g.get("title"):
        add("title", h.get("title"), g.get("title"))
    if g.get("setAtInstall") and not h.get("setAtInstall"):
        add("set-at-install-marker", None, "x-set-at-install: true")
    if not jeq(h.get("conditionals"), g.get("conditionals")):
        add("conditional-blocks" if same_leaves(h.get("conditionals"), g.get("conditionals"), h, g) else "conditional", h.get("conditionals"), g.get("conditionals"))
    if not jeq(h.get("anyOf"), g.get("anyOf")):
        add("union", h.get("anyOf"), g.get("anyOf"))
    if not jeq(h.get("other"), g.get("other")):
        add("keyword", h.get("other"), g.get("other"))

    hprops, gprops = h.get("props", {}), g.get("props", {})
    hreq, greq = set(h.get("required", [])), set(g.get("required", []))
    for name in sorted(set(hprops) | set(gprops)):
        sub = f"{path}.{name}" if path else name
        if name not in gprops:
            out.append(Diff(doc, sub, "property-missing", "gap", "present", "absent", CAUSES["property-missing"]))
        elif name not in hprops:
            out.append(Diff(doc, sub, "property-extra", "gap", "absent", "present", CAUSES["property-extra"]))
        else:
            compare(doc, hprops[name], gprops[name], sub, out, has_default, expand)
            if name in hreq and name not in greq:
                given = has_default(sub, hprops[name], gprops[name]) if has_default else "default" in gprops[name]
                cat = "required-to-default" if given else "required-to-optional"
                out.append(Diff(doc, sub, cat, "expected" if cat in EXPECTED else "gap", "required", "optional", CAUSES[cat]))
            elif name in greq and name not in hreq:
                out.append(Diff(doc, sub, "optional-to-required", "gap", "optional", "required", CAUSES["optional-to-required"]))
    for key in ("items", "additional", "propertyNames"):
        if key in h and key in g:
            compare(doc, h[key], g[key], f"{path}[]" if key == "items" else f"{path}{{}}" if key == "additional" else f"{path}<name>", out, has_default, expand)
        elif key in h or key in g:
            out.append(Diff(doc, f"{path}", "keyword", "gap", key if key in h else None, key if key in g else None, CAUSES["keyword"]))


# ------------------------------------------------------------------ defaults


def readme_rows(readme: Path) -> dict[str, object]:
    """Every value the chart's contract has, from the values table the helm
    generator writes beside the schema, with the default the CONTRACT states
    (the schema itself carries none: see the `default-missing` cause)."""
    out: dict[str, object] = {}
    if not readme.exists():
        return out
    for line in readme.read_text(encoding="utf-8").splitlines():
        m = re.match(r"^\| `([^`]+)` \| .*? \| (`(?:[^`]|\\`)*`)? ?\|", line)
        if m:
            raw = m.group(2)
            if raw is None:
                out[m.group(1)] = MISSING
                continue
            # the table escapes a bar in a cell as `\|` (pkl-contracts v0.3.1)
            text = raw[1:-1].replace("\\|", "|")
            text = {"Map()": "{}", "List()": "[]"}.get(text, text)
            try:
                out[m.group(1)] = json.loads(text)
            except ValueError:
                out[m.group(1)] = text
    return out


def set_at_install(readme: Path) -> set[str]:
    """The values the contract marks `@A.SetAtInstall`: required by the schema and
    absent from the defaults, so the values table says so."""
    if not readme.exists():
        return set()
    return {m.group(1) for line in readme.read_text(encoding="utf-8").splitlines() if (m := re.match(r"^\| `([^`]+)` \| .*? \|\s*\| \*\*Set at install\.\*\*", line))}


def walk_defaults(chart: str, values, rows: dict, path: str, out: list[Diff], at_install: frozenset | set = frozenset()):
    """What values.yaml sets, against the defaults the contract states."""
    doc = chart + " values.yaml"
    if isinstance(values, dict) and any(r.startswith(path + ".") if path else True for r in rows):
        for k, v in values.items():
            walk_defaults(chart, v, rows, f"{path}.{k}" if path else k, out, at_install)
        return
    stated = rows.get(path, MISSING)
    if stated is not MISSING:
        if not jeq(stated, values):
            out.append(Diff(doc, path, "default", "gap", values, stated, CAUSES["default"]))
        return
    if values == "" and path in at_install:
        out.append(Diff(doc, path, "set-at-install", "expected", values, None, CAUSES["set-at-install"]))
        return
    if isinstance(values, dict) and values:
        cause = "the contract states no default for this object (an object default is expressible since pkl-contracts v0.3.0; the property is not modelled, or has none)"
    elif values == "":
        cause = "values.yaml ships an empty string for a field the contract requires non-empty (required, no default)"
    else:
        cause = "the contract states no default here"
    out.append(Diff(doc, path, "default-missing", "gap", values, None, cause))


def diff_values_files(chart: str, hand, gen, path: str, out: list[Diff]):
    if isinstance(hand, dict) and isinstance(gen, dict):
        for k in sorted(set(hand) | set(gen)):
            sub = f"{path}.{k}" if path else k
            if k not in gen:
                out.append(Diff(chart + " values.yaml (file)", sub, "default-missing", "gap", hand[k], None, "the generated values.yaml lacks a key the hand-written one has"))
            elif k not in hand:
                out.append(Diff(chart + " values.yaml (file)", sub, "default-added", "structural", None, gen[k], "the generated values.yaml has a key the hand-written one lacks"))
            else:
                diff_values_files(chart, hand[k], gen[k], sub, out)
    elif not jeq(hand, gen):
        out.append(Diff(chart + " values.yaml (file)", path, "default", "gap", hand, gen, "the generated values.yaml says something else"))


# ------------------------------------------------------------------ verdicts


def deep_merge(base, over):
    if isinstance(base, dict) and isinstance(over, dict):
        out = dict(base)
        for k, v in over.items():
            if v is None:
                out.pop(k, None)  # Helm: null deletes the key
            else:
                out[k] = deep_merge(base.get(k), v) if k in base else v
        return out
    return over


def verdict(validator_cls, schema, reg, doc) -> str | None:
    """None when valid, else the first error's path and message."""
    from referencing import Registry as Reg
    from referencing import Resource
    from referencing.jsonschema import DRAFT202012

    r = Reg()
    for ident, d in reg.by_id.items():
        r = r.with_resource(ident, Resource.from_contents(d, default_specification=DRAFT202012))
    v = validator_cls(schema, registry=r)
    errs = sorted(v.iter_errors(doc), key=lambda e: list(map(str, e.absolute_path)))
    if not errs:
        return None
    e = errs[0]
    return ".".join(map(str, e.absolute_path)) + ": " + e.message[:80]


# ---------------------------------------------------------------------- main


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("generated", type=Path)
    ap.add_argument("--report", type=Path)
    ap.add_argument("--summary", type=Path)
    ap.add_argument("--no-verdicts", action="store_true")
    args = ap.parse_args()
    gen_dir: Path = args.generated

    hand_reg, gen_reg = Registry(), Registry()
    pairs: list[tuple[str, dict | None, dict | None, str]] = []  # name, hand doc, generated doc, kind
    problems: list[str] = []

    def need(p: Path):
        if not p.exists():
            problems.append(f"missing: {rel(p)}")
            return None
        return load_json(p)

    docs = [(n, h, g) for n, h, g in CONFIGS]
    docs += [(f"fragments/{p.name[:-5]}", p, f"schemas/fragments/{p.name}") for p in FRAGMENTS]
    for name, hpath, gfile in docs:
        # A fragment the contract package does not model yet has no generated
        # twin: that is a `document-missing` gap in the report, not a failure.
        optional = name in NO_PKL_TWIN and not (gen_dir / gfile).exists()
        h, g = need(hpath), None if optional else need(gen_dir / gfile)
        if h is not None:
            hand_reg.add(h)
        if g is not None:
            gen_reg.add(g)
        pairs.append((name, h, g, "document"))
    # generated documents with no hand-written twin
    have = {gfile for _, _, gfile in docs}
    for p in sorted((gen_dir / "schemas").rglob("*.json")):
        f = str(p.relative_to(gen_dir))
        if f not in have:
            gen_reg.add(load_json(p))
            pairs.append((f[len("schemas/"):-5], None, load_json(p), "document"))
    chart_docs = {}
    for chart, (hpath, _) in CHART_FILES.items():
        h, g = need(hpath), need(gen_dir / "charts" / chart / "values.schema.json")
        if h is not None:
            hand_reg.add(h)
        if g is not None:
            gen_reg.add(g)
        chart_docs[chart] = (h, g)
        pairs.append((f"chart {chart}", h, g, "chart"))
    if problems:
        print("shadow-diff: cannot compare:\n  " + "\n  ".join(problems), file=sys.stderr)
        return 1

    nh, ng = Normaliser(hand_reg), Normaliser(gen_reg)
    diffs: list[Diff] = []
    for name, h, g, kind in pairs:
        if h is None:
            diffs.append(Diff(name, "(document)", "document-extra", "gap", None, "generated", CAUSES["document-extra"]))
            continue
        if g is None:
            diffs.append(Diff(name, "(document)", "document-missing", "gap", "present", None, CAUSES["document-missing"]))
            continue
        hn = nh.norm(h, h.get("$id", ""), h)
        gn = ng.norm(g, g.get("$id", ""), g)
        chart = name[len("chart "):] if kind == "chart" else None
        stated = {k for k, v in readme_rows(gen_dir / "charts" / chart / "README.md").items() if v is not MISSING} if chart else None
        # Whether the CONTRACT gives a field a default, which the schemas do not
        # say: a chart's values table does; for a document, the hand-written
        # schema's own default is what a field made optional by one stands for.
        given = (
            (lambda path, hnode, gnode: path in stated or "default" in gnode)
            if chart
            else (lambda path, hnode, gnode: "default" in hnode or "default" in gnode)
        )
        compare(name, hn, gn, "", diffs, given, (nh.expand, ng.expand))

    # defaults
    for chart, (hpath, vpath) in CHART_FILES.items():
        gv = gen_dir / "charts" / chart / "values.yaml"
        if gv.exists():
            # the defaults are modelled: the generated file is compared whole
            diff_values_files(chart, load_yaml(vpath) or {}, load_yaml(gv) or {}, "", diffs)
        else:
            walk_defaults(chart, load_yaml(vpath) or {}, readme_rows(gen_dir / "charts" / chart / "README.md"), "", diffs, set_at_install(gen_dir / "charts" / chart / "README.md"))

    hand_by_name = {n: h for n, h, _, k in pairs if k == "document"}
    gen_by_name = {n: g for n, _, g, k in pairs if k == "document"}

    # verdicts
    verdicts: list[tuple[str, str, str | None, str | None]] = []
    if not args.no_verdicts:
        try:
            from jsonschema import Draft202012Validator as V
        except ImportError:
            print("shadow-diff: jsonschema is not importable; verdicts skipped (run through `just shadow-diff`)", file=sys.stderr)
            V = None
        if V is not None:
            def run(label, doc_name, hschema, gschema, instance):
                verdicts.append((label, doc_name, verdict(V, hschema, hand_reg, instance), verdict(V, gschema, gen_reg, instance)))

            for name, files in CONFIG_FIXTURES.items():
                for f in files:
                    run(rel(f), name, hand_by_name[name], gen_by_name[name], load_yaml(f))
            for chart, files in CHART_FIXTURES.items():
                base = load_yaml(CHART_FILES[chart][1]) or {}
                h, g = chart_docs[chart]
                for f in files:
                    run(rel(f), f"chart {chart}", h, g, deep_merge(base, load_yaml(f) or {}))

    # The semantic rules of decision 0010, one instance each, through both
    # schemas: the claim that a difference is "expected" is then a measurement.
    probe_rows: list[tuple[str, str, str | None, str | None, bool]] = []
    if verdicts or not args.no_verdicts:
        try:
            from jsonschema import Draft202012Validator as V2
        except ImportError:
            V2 = None
        if V2 is not None:
            web = {"probes": {"address": ":7070"}, "listen": {"address": ":8080"}, "urls": {"address": "urls:80"}, "assets": {"directory": "/a"}}
            prober = {"probes": {"address": ":7070"}, "interval": "10s", "urls": {"address": "u"}, "redirect": {"address": "r"}}
            for rule, label, doc_name, instance, differ in [
                ("a field with a default is optional", "web: `faro: {}` (no `enabled`)", "web", {**web, "faro": {}}, True),
                ("a pattern refuses a newline", "prober: `interval: \"10s\\n\"`", "prober", {**prober, "interval": "10s\n"}, True),
            ]:  # fmt: skip
                hs = hand_by_name.get(doc_name) if doc_name in hand_by_name else chart_docs[doc_name[len("chart "):]][0]
                gs = gen_by_name.get(doc_name) if doc_name in gen_by_name else chart_docs[doc_name[len("chart "):]][1]
                hv, gv = verdict(V2, hs, hand_reg, instance), verdict(V2, gs, gen_reg, instance)
                probe_rows.append((rule, label, hv, gv, ((hv is None) != (gv is None)) == differ))

    # ------------------------------------------------------------- the report
    diffs.sort(key=lambda d: (d.doc, d.path, d.category))
    by_cat, by_class, by_doc = Counter(), Counter(), Counter()
    for d in diffs:
        by_cat[d.category] += 1
        by_class[d.klass] += 1
        by_doc[d.doc] += 1
    agree = sum(1 for _, _, h, g in verdicts if (h is None) == (g is None))
    for k in ("expected", "structural", "gap", "doc"):
        by_class.setdefault(k, 0)
    summary = {
        "schema": 1,
        "documents": sum(1 for p in pairs if p[3] == "document"),
        "charts": sum(1 for p in pairs if p[3] == "chart"),
        "differences": len(diffs),
        "byClass": dict(sorted(by_class.items())),
        "byCategory": dict(sorted(by_cat.items())),
        "byDocument": dict(sorted(by_doc.items())),
        "verdicts": {"fixtures": len(verdicts), "agree": agree, "disagree": len(verdicts) - agree},
        "probes": {"rules": len(probe_rows), "asPredicted": sum(1 for r in probe_rows if r[4])},
    }
    lines = ["# Shadow diff: generated against hand-written", ""]
    lines += [
        "Report only. The hand-written schemas are authoritative; nothing here fails a build.",
        "",
        f"**{len(diffs)} differences** in {summary['documents']} documents and {summary['charts']} chart schemas:"
        f" {by_class['expected']} expected (decision 0010's semantic rules), {by_class['structural']} structural (gone at the switch), {by_class['gap']} gaps, {by_class['doc']} documentation only.",
        "",
        "| Category | Class | Count |",
        "|---|---|---:|",
    ]
    cat_class = {}
    for d in diffs:
        cat_class[d.category] = d.klass
    for c, n in sorted(by_cat.items(), key=lambda x: (x[1] * -1, x[0])):
        lines.append(f"| `{c}` | {cat_class[c]} | {n} |")
    if verdicts:
        lines += ["", f"Verdicts on {len(verdicts)} existing fixtures: **{agree} agree**, {len(verdicts) - agree} disagree.", ""]
    def table(rows: list[Diff]) -> list[str]:
        out, cur = [], None
        for d in rows:
            if d.doc != cur:
                cur = d.doc
                out += ["", f"### {cur}", "", "| Path | Category | Class | Hand-written | Generated | Cause |", "|---|---|---|---|---|---|"]
            out.append(f"| `{d.path}` | `{d.category}` | {d.klass} | {short(d.hand)} | {short(d.gen)} | {d.cause} |")
        return out

    lines += ["", "## Differences", ""]
    lines += table([d for d in diffs if d.klass not in ("doc", "structural")]) or ["None."]
    structural = [d for d in diffs if d.klass == "structural"]
    if structural:
        lines += ["", "<details><summary>Structural differences (%d)</summary>" % len(structural), ""]
        lines += table(structural)
        lines += ["", "</details>"]
    docs_only = [d for d in diffs if d.klass == "doc"]
    if docs_only:
        lines += ["", "<details><summary>Documentation-only differences (%d)</summary>" % len(docs_only), ""]
        lines += table(docs_only)
        lines += ["", "</details>"]
    if probe_rows:
        lines += ["", "## The semantic rules, probed", "",
                  "One instance per rule decision 0010 states, through both schemas: where the two disagree, the rule is the cause.", "",
                  "| Rule | Instance | Hand-written | Generated | As the rule predicts |", "|---|---|---|---|---|"]  # fmt: skip
        for rule, where, hv, gv, predicted in probe_rows:
            lines.append(f"| {rule} | {where} | {'accepts' if hv is None else 'refuses'} | {'accepts' if gv is None else 'refuses'} | {'yes' if predicted else '**NO**'} |")
    if verdicts:
        lines += ["", "## Verdicts", "", "Each fixture, laid over the chart's values.yaml where it is a chart's, through both schemas.", ""]
        lines += ["| Fixture | Document | Hand-written | Generated | Agree |", "|---|---|---|---|---|"]
        for label, doc_name, hv, gv in verdicts:
            ok = (hv is None) == (gv is None)
            lines.append(f"| `{label}` | {doc_name} | {'accepts' if hv is None else 'refuses: ' + short(hv)} | {'accepts' if gv is None else 'refuses: ' + short(gv)} | {'yes' if ok else '**NO**'} |")  # fmt: skip
    lines += ["", "## Summary (machine-readable)", "", "```json", json.dumps(summary, indent=2, sort_keys=True), "```", ""]
    report = "\n".join(lines)
    if args.report:
        args.report.write_text(report, encoding="utf-8")
    else:
        print(report)
    if args.summary:
        args.summary.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print("SHADOW_SUMMARY " + json.dumps(summary, sort_keys=True, separators=(",", ":")), file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
