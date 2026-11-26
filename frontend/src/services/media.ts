// Mirrors internal/media: Route, VaultRoute (where stored media/vault are served) and ThumbQuery.
const MEDIA_ROUTE = '/flow-media/';
const VAULT_ROUTE = '/muse-vault/';
const THUMB_QUERY = 'thumb';

/**
 * A small JPEG of a stored image for list cells (made once by the backend);
 * other URLs (blob:, data:, videos the backend skips) come back as they are.
 */
export function thumbUrl(url: string): string;
export function thumbUrl(url: string | undefined): string | undefined;
export function thumbUrl(url: string | undefined): string | undefined {
  if (!url?.startsWith(MEDIA_ROUTE) && !url?.startsWith(VAULT_ROUTE)) return url;
  return `${url}${url.includes('?') ? '&' : '?'}${THUMB_QUERY}`;
}
