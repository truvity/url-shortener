// Command render-diff compares two `helm template` renders object by object.
//
// It is the proof a chart change needs when the objects it renders are live:
// "the render did not move" for every object a release owns, or each
// difference named. Both inputs are multi-document YAML as `helm template`
// prints it. Each object is keyed by apiVersion group, kind, namespace and
// name; the namespace defaults to the one given with -namespace on both
// sides, because an object that leaves it unset is placed in the release
// namespace by Helm and the two renders mean the same thing.
//
// Every object present on only one side is reported, then, for each object on
// both, every field (labels and annotations included) that differs, as
// `path: old -> new`. Exit status 1 when anything differs, so a script can
// require an empty diff; -allow takes a regular expression of differences
// that have been explained (matched against `key path`), which are listed but
// do not fail.
//
//	go run ./hack/render-diff -namespace NS [-allow RE] OLD.yaml NEW.yaml
package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

func main() {
	ns := flag.String("namespace", "default", "the release namespace, assumed for an object that sets none")
	allow := flag.String("allow", "", "regular expression of explained differences, matched against `object path`")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: render-diff -namespace NS [-allow RE] OLD.yaml NEW.yaml")
		os.Exit(2)
	}
	var allowed *regexp.Regexp
	if *allow != "" {
		allowed = regexp.MustCompile(*allow)
	}
	oldObjs, err := load(flag.Arg(0), *ns)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	newObjs, err := load(flag.Arg(1), *ns)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	keys := map[string]bool{}
	for k := range oldObjs {
		keys[k] = true
	}
	for k := range newObjs {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	bad, explained, same := 0, 0, 0
	for _, k := range sorted {
		o, inOld := oldObjs[k]
		n, inNew := newObjs[k]
		switch {
		case inOld && !inNew:
			fmt.Printf("REMOVED  %s\n", k)
			bad++
		case !inOld && inNew:
			fmt.Printf("ADDED    %s\n", k)
			bad++
		default:
			fo, fn := flatten(o), flatten(n)
			paths := map[string]bool{}
			for p := range fo {
				paths[p] = true
			}
			for p := range fn {
				paths[p] = true
			}
			var ps []string
			for p := range paths {
				if fo[p] != fn[p] {
					ps = append(ps, p)
				}
			}
			sort.Strings(ps)
			if len(ps) == 0 {
				same++
				fmt.Printf("SAME     %s\n", k)
				continue
			}
			for _, p := range ps {
				o, n := fo[p], fn[p]
				if o == "" {
					o = "(absent)"
				}
				if n == "" {
					n = "(absent)"
				}
				tag := "DIFF    "
				if allowed != nil && allowed.MatchString(k+" "+p) {
					tag = "EXPLAINED"
					explained++
				} else {
					bad++
				}
				fmt.Printf("%s %s  %s: %s -> %s\n", tag, k, p, o, n)
			}
		}
	}
	fmt.Printf("\n%d object(s) identical, %d explained difference(s), %d unexplained\n", same, explained, bad)
	if bad > 0 {
		os.Exit(1)
	}
}

func load(path, ns string) (map[string]map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]any{}
	for _, doc := range strings.Split("\n"+string(raw), "\n---") {
		var obj map[string]any
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if len(obj) == 0 {
			continue
		}
		md, _ := obj["metadata"].(map[string]any)
		if md == nil {
			md = map[string]any{}
			obj["metadata"] = md
		}
		if _, ok := md["namespace"]; !ok {
			md["namespace"] = ns
		}
		group := fmt.Sprint(obj["apiVersion"])
		if i := strings.Index(group, "/"); i >= 0 {
			group = group[:i]
		} else {
			group = ""
		}
		key := fmt.Sprintf("%s/%s %s/%s", group, obj["kind"], md["namespace"], md["name"])
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("%s: %s is rendered twice", path, key)
		}
		out[key] = obj
	}
	return out, nil
}

// flatten turns an object into `path -> value` lines. Lists are indexed;
// the namespace default is already folded in.
func flatten(v any) map[string]string {
	m := map[string]string{}
	var walk func(p string, v any)
	walk = func(p string, v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, c := range t {
				walk(p+"."+k, c)
			}
		case []any:
			for i, c := range t {
				walk(fmt.Sprintf("%s[%d]", p, i), c)
			}
		default:
			m[strings.TrimPrefix(p, ".")] = fmt.Sprintf("%v", t)
		}
	}
	walk("", v)
	return m
}
