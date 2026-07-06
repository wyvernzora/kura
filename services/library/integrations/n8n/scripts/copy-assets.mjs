import { access, cp, readdir } from 'node:fs/promises';
import { join } from 'node:path';

const ICON = '../../docs/assets/logo-n8n.svg';
const NODES = 'dist/nodes';
const CREDENTIALS = 'dist/credentials';

await access(ICON);

let n = 0;
for (const entry of await readdir(NODES, { withFileTypes: true })) {
	if (!entry.isDirectory()) continue;
	await cp(ICON, join(NODES, entry.name, 'kura.svg'));
	n++;
}

await cp(ICON, join(CREDENTIALS, 'kura.svg'));
n++;

console.log(`copy-assets: placed kura.svg in ${n} node/credential dir(s) from ${ICON}`);
