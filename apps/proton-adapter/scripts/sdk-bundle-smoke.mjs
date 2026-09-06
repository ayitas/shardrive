import { build } from 'esbuild';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';

const directory = await mkdtemp(join(process.cwd(), '.sdk-smoke-'));
const entry = join(directory, 'entry.mjs');
const output = join(directory, 'bundle.mjs');

try {
	await writeFile(
		entry,
		"import { ProtonDriveClient, VERSION } from '@protontech/drive-sdk';\n" +
			"if (typeof ProtonDriveClient !== 'function' || typeof VERSION !== 'string') throw new Error('unexpected SDK public exports');\n"
	);
	await build({ entryPoints: [entry], bundle: true, platform: 'node', format: 'esm', outfile: output });
	await import(`${pathToFileURL(output).href}?smoke=1`);
	console.log('Proton SDK bundle smoke passed');
} finally {
	await rm(directory, { recursive: true, force: true });
}
