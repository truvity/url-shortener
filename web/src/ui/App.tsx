import { useState } from "react";

interface Answer {
  key?: string;
  longUrl?: string;
  clicks?: number;
  error?: string;
}

/**
 * The whole front end: ask about one key, show what came back.
 *
 * Deliberately small. What this example is demonstrating is the seam — a
 * page that holds no database credential and asks the service that owns the
 * table — and a larger interface would bury it.
 */
export function App(): React.JSX.Element {
  const [key, setKey] = useState("");
  const [answer, setAnswer] = useState<Answer | undefined>();
  const [asking, setAsking] = useState(false);

  async function ask(event: React.FormEvent): Promise<void> {
    event.preventDefault();
    setAsking(true);
    try {
      const response = await fetch(`/api/url?key=${encodeURIComponent(key)}`);
      setAnswer((await response.json()) as Answer);
    } catch {
      setAnswer({ error: "the front end could not reach its own server" });
    } finally {
      setAsking(false);
    }
  }

  return (
    <main>
      <h1>url-shortener</h1>
      <form onSubmit={(e) => void ask(e)}>
        <label htmlFor="key">Short key</label>
        <input id="key" value={key} onChange={(e) => setKey(e.target.value)} placeholder="abc12345" />
        <button type="submit" disabled={asking || key.length === 0}>
          {asking ? "asking…" : "Look it up"}
        </button>
      </form>
      {answer?.error !== undefined && <p role="alert">{answer.error}</p>}
      {answer?.longUrl !== undefined && (
        <dl>
          <dt>Goes to</dt>
          <dd>{answer.longUrl}</dd>
          <dt>Followed</dt>
          <dd>{answer.clicks ?? 0} times</dd>
        </dl>
      )}
    </main>
  );
}
