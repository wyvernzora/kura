import { access, cp } from 'node:fs/promises';

const SOURCE = 'src/assets';
const DESTINATION = 'dist/assets';

await access(SOURCE);
await cp(SOURCE, DESTINATION, { recursive: true });

console.log(`copy-assets: copied ${SOURCE} to ${DESTINATION}`);
