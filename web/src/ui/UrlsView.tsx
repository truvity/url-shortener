import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import Link from "@mui/material/Link";
import Paper from "@mui/material/Paper";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableContainer from "@mui/material/TableContainer";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { type FormEvent, useEffect, useMemo, useState } from "react";
import { createUrl, deleteUrl, listUrls, type UrlInfo } from "./api.ts";

/** Exactly what the service will accept, so the refusal arrives here. */
const KEY_SHAPE = /^[a-zA-Z0-9]{8}$/;

/**
 * The shortener's whole surface: create, list with click counts, retire.
 *
 * It holds no database credential and no address for the service that owns
 * the table. Every line here goes through this app's own server, which is
 * the seam this example is about — a front end that asks rather than reads.
 */
export function UrlsView(): React.JSX.Element {
  const [reload, setReload] = useState(0);
  const [urls, setUrls] = useState<UrlInfo[] | null>(null);
  const [error, setError] = useState("");
  const [actionError, setActionError] = useState("");
  const [longUrl, setLongUrl] = useState("");
  const [customKey, setCustomKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [filter, setFilter] = useState("");
  const refresh = (): void => setReload((r) => r + 1);

  useEffect(() => {
    // Aborted on unmount, so a slow answer to a question nobody is asking
    // any more does not set state on a component that is gone.
    const cancel = new AbortController();
    setError("");
    listUrls(cancel.signal)
      .then(setUrls)
      .catch((e: Error) => {
        if (e.name !== "AbortError") setError(String(e.message));
      });
    return () => cancel.abort();
  }, [reload]);

  const submit = async (event: FormEvent): Promise<void> => {
    event.preventDefault();
    setActionError("");
    const key = customKey.trim();
    // The service's rule, checked here too. Not instead of there — the
    // service still refuses — but so a typo answers immediately rather
    // than after a round trip.
    if (key && !KEY_SHAPE.test(key)) {
      setActionError("A custom key is exactly 8 letters or digits.");
      return;
    }
    setBusy(true);
    try {
      await createUrl(longUrl.trim(), key || undefined);
      setLongUrl("");
      setCustomKey("");
      refresh();
    } catch (e) {
      setActionError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (key: string): Promise<void> => {
    if (!window.confirm(`Retire /r/${key}?`)) return;
    setActionError("");
    try {
      await deleteUrl(key);
      refresh();
    } catch (e) {
      setActionError(e instanceof Error ? e.message : String(e));
    }
  };

  const rows = useMemo(
    () =>
      (urls ?? []).filter(
        (u) => !filter || `${u.key} ${u.longUrl}`.toLowerCase().includes(filter.toLowerCase()),
      ),
    [urls, filter],
  );

  if (error) {
    return (
      <Alert
        severity="error"
        action={
          <Button color="inherit" onClick={refresh}>
            Retry
          </Button>
        }
      >
        {error}
      </Alert>
    );
  }
  if (urls === null) return <Typography color="text.secondary">Loading…</Typography>;

  return (
    <>
      <Paper variant="outlined" sx={{ p: 2, mb: 3 }}>
        <Box
          component="form"
          onSubmit={submit}
          sx={{ display: "flex", gap: 1.5, flexWrap: "wrap", alignItems: "center" }}
        >
          <TextField
            label="Long URL"
            type="url"
            required
            value={longUrl}
            onChange={(e) => setLongUrl(e.target.value)}
            sx={{ flex: 1, minWidth: 260 }}
            placeholder="https://example.com"
          />
          <TextField
            label="Custom key (optional)"
            value={customKey}
            onChange={(e) => setCustomKey(e.target.value)}
            slotProps={{ htmlInput: { maxLength: 8 } }}
            sx={{ width: 180 }}
            placeholder="abc12345"
          />
          <Button type="submit" variant="contained" disabled={busy}>
            {busy ? "Shortening…" : "Shorten"}
          </Button>
        </Box>
      </Paper>

      {actionError && (
        <Alert severity="error" sx={{ mb: 2 }} onClose={() => setActionError("")}>
          {actionError}
        </Alert>
      )}

      <Box sx={{ display: "flex", alignItems: "center", gap: 1, mb: 1 }}>
        <Typography variant="h6" sx={{ flex: 1 }}>
          Short URLs
        </Typography>
        <TextField
          placeholder="filter…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          sx={{ width: 200 }}
        />
      </Box>

      <TableContainer component={Paper} variant="outlined">
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell>Short</TableCell>
              <TableCell>Target</TableCell>
              <TableCell align="right">Clicks</TableCell>
              <TableCell>Created</TableCell>
              <TableCell />
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.map((u) => (
              <TableRow key={u.key} hover>
                <TableCell>
                  <Link href={`/r/${u.key}`} sx={{ fontFamily: "monospace" }}>
                    /r/{u.key}
                  </Link>
                </TableCell>
                <TableCell
                  sx={{
                    maxWidth: 420,
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    whiteSpace: "nowrap",
                  }}
                >
                  <Typography variant="body2" color="text.secondary" title={u.longUrl}>
                    {u.longUrl}
                  </Typography>
                </TableCell>
                <TableCell align="right">
                  <Chip label={u.clicks} variant="outlined" />
                </TableCell>
                <TableCell>
                  <Typography variant="body2" color="text.secondary">
                    {u.createdAt ? new Date(u.createdAt).toLocaleString() : "—"}
                  </Typography>
                </TableCell>
                <TableCell align="right">
                  <Button color="error" onClick={() => remove(u.key)}>
                    Retire
                  </Button>
                </TableCell>
              </TableRow>
            ))}
            {rows.length === 0 && (
              <TableRow>
                <TableCell colSpan={5}>
                  <Typography color="text.secondary">
                    {filter ? "Nothing matches." : "No short URLs yet — create one above."}
                  </Typography>
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </TableContainer>
    </>
  );
}
