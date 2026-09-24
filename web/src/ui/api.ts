/**
 * The page's whole view of the back end.
 *
 * Every call goes to this server, which asks the service that owns the
 * table. The browser never reaches that service and this file holds no
 * address for it — the seam the example exists to show, kept visible by
 * there being nothing else here.
 */

export interface UrlInfo {
  key: string;
  longUrl: string;
  clicks: number;
  createdAt: string | null;
  deleted: boolean;
}

/** A refusal the SERVICE wrote, not one this file invented. */
async function refusal(response: Response): Promise<never> {
  let detail = `${response.status}`;
  try {
    const body = (await response.json()) as { error?: string };
    if (body.error) detail = body.error;
  } catch {
    // A non-JSON body is not worth a second failure on top of the first.
  }
  throw new Error(detail);
}

export async function listUrls(signal?: AbortSignal): Promise<UrlInfo[]> {
  const response = await fetch("/api/urls", { signal });
  if (!response.ok) return refusal(response);
  const body = (await response.json()) as { urls: UrlInfo[] };
  return body.urls;
}

export async function createUrl(longUrl: string, key?: string): Promise<UrlInfo> {
  const response = await fetch("/api/urls", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ longUrl, key: key ?? "" }),
  });
  if (!response.ok) return refusal(response);
  return (await response.json()) as UrlInfo;
}

export async function deleteUrl(key: string): Promise<void> {
  const response = await fetch(`/api/urls/${encodeURIComponent(key)}`, { method: "DELETE" });
  if (!response.ok) await refusal(response);
}
