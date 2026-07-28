const LIBRARY_ROOT = `\${KURA_LIBRARY_ROOT}`;
const INBOX_ROOT = `\${KURA_INBOX_ROOT}`;

export interface ParsedMediaPath {
  scheme: string;
  rel: string;
  fileName: string;
  ext: string;
  portable: string;
}

export function parseMediaPath(tagged: string, seriesDir?: string): ParsedMediaPath {
  const colon = tagged.indexOf(':');
  const scheme = colon >= 0 ? tagged.slice(0, colon) : '';
  const rel = colon >= 0 ? tagged.slice(colon + 1) : tagged;
  const fileName = rel.split('/').at(-1) ?? '';
  const dot = fileName.lastIndexOf('.');
  const ext = dot > 0 ? fileName.slice(dot + 1) : '';

  let portable = tagged;
  if (scheme === 'series') {
    portable = `${LIBRARY_ROOT}/${seriesDir || '<series-directory>'}/${rel}`;
  } else if (scheme === 'inbox') {
    portable = `${INBOX_ROOT}/${rel}`;
  }

  return { scheme, rel, fileName, ext, portable };
}
