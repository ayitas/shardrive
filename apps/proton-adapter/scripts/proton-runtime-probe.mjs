import { execFile, execFileSync } from 'node:child_process';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);
const sourceDir = process.env.PROTON_SDK_SOURCE_DIR;
const expectedCommit = process.env.PROTON_SDK_EXPECTED_COMMIT ?? 'c8d03244938a6b4d107c755df8904d7d971ed1c2';

if (!sourceDir) throw new Error('PROTON_SDK_SOURCE_DIR must point to the official Proton SDK source tree');
const resolvedSourceDir = path.resolve(sourceDir);
const actualCommit = execFileSync('git', ['-C', resolvedSourceDir, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
if (actualCommit !== expectedCommit) throw new Error(`official Proton SDK commit mismatch: expected ${expectedCommit}, got ${actualCommit}`);

// Keep the generated entry under the official CLI workspace so Bun resolves
// the workspace-installed crypto/ky dependencies exactly as the official CLI.
const tempDir = await mkdtemp(path.join(resolvedSourceDir, 'cli', '.shardrive-runtime-'));
const entryPath = path.join(tempDir, 'entry.ts');
const bundlePath = path.join(tempDir, 'bundle.js');
const sdkIndex = JSON.stringify(`${resolvedSourceDir}/client/js/src/index.ts`);
const accountIndex = JSON.stringify(`${resolvedSourceDir}/incubating/account/js/src/index.ts`);
const entry = `
import { CryptoProxy } from '@protontech/crypto';
import { Api as CryptoApi } from '@protontech/crypto/proxy/endpoint/api.ts';
import { MemoryCache, NullFeatureFlagProvider, OpenPGPCryptoWithCryptoProxy, ProtonDriveClient } from ${sdkIndex};
import { ApiClient, initAccount } from ${accountIndex};
class Credentials {
  private callbacks = new Set<() => void>();
  constructor(private snapshot: { userKeyPassword: string; session: { uid: string; accessToken: string; refreshToken?: string }; telemetryEnabled?: boolean }) {}
  get uid() { return this.snapshot.session.uid; } get accessToken() { return this.snapshot.session.accessToken; } get refreshToken() { return this.snapshot.session.refreshToken; }
  on(_: 'sessionInfoChanged', callback: () => void) { this.callbacks.add(callback); } isLoggedIn() { return true; } isTelemetryEnabled() { return this.snapshot.telemetryEnabled ?? false; }
  getUserKeyPassword() { return this.snapshot.userKeyPassword; } async load() {}
  async setUserKeyPassword(value: string) { this.snapshot.userKeyPassword = value; this.callbacks.forEach((callback) => callback()); }
  async setSessionInfo(value: { uid: string; accessToken: string; refreshToken?: string }) { this.snapshot.session = value; this.callbacks.forEach((callback) => callback()); }
  async setTelemetryEnabled(value: boolean) { this.snapshot.telemetryEnabled = value; } async signOut() {}
}
const raw = await Bun.secrets.get({ service: 'ch.proton.drive/drive-sdk-cli', name: 'auth-session' });
if (!raw) throw new Error('official Proton CLI session not found');
const credentials = new Credentials(JSON.parse(raw));
const logger = { debug() {}, info() {}, warn() {}, error() {} };
const apiClient = new ApiClient({ baseUrl: 'drive-api.proton.me', appVersion: 'cli-drive@0.8.0', credentials, logger, headers: { 'x-pm-drive-sdk-version': '0.21.0' } });
CryptoApi.init({}); CryptoProxy.setEndpoint(new CryptoApi(), (endpoint) => endpoint.clearKeyStore());
const { addresses, srp } = await initAccount({ authClientId: 'cli-drive', apiClient, credentials, cryptoProxy: CryptoProxy, logger });
const client = new ProtonDriveClient({
  config: { baseUrl: 'drive-api.proton.me', clientUid: 'shardrive-runtime-probe' },
  httpClient: {
    fetchJson: (request) => apiClient.authenticatedRequest(request.url, { method: request.method, headers: request.headers, ...(request.json !== undefined ? { json: request.json } : {}), ...(request.body !== undefined && request.json === undefined ? { body: request.body } : {}), timeout: request.timeoutMs, signal: request.signal, throwHttpErrors: false }),
    fetchBlob: (request) => apiClient.authenticatedRequest(request.url, { method: request.method, headers: request.headers, body: request.body, timeout: request.timeoutMs, signal: request.signal, throwHttpErrors: false }),
  },
  entitiesCache: new MemoryCache(), cryptoCache: new MemoryCache(), openPGPCryptoModule: new OpenPGPCryptoWithCryptoProxy(CryptoProxy),
  account: { getOwnPrimaryAddress: () => addresses.getOwnPrimaryAddress(), getOwnAddresses: () => addresses.getOwnAddresses(), getOwnAddress: (value: string) => addresses.getOwnAddress(value), hasProtonAccount: (value: string) => addresses.hasProtonAccount(value), getPublicKeys: (value: string, forceRefresh?: boolean) => addresses.getPublicKeys(value, forceRefresh) },
  srpModule: srp, featureFlagProvider: new NullFeatureFlagProvider(),
});
const root = await client.getMyFilesRootFolder();
console.log(JSON.stringify({ constructed: true, rootUidLength: root.uid.length }));
`;
await writeFile(entryPath, entry, { mode: 0o600 });

try {
  const bun = process.env.PROTON_BUN_BIN ?? 'bun';
  try {
    await run(bun, ['build', entryPath, '--target=bun', '--format=esm', `--outfile=${bundlePath}`]);
    await run(bun, [bundlePath]);
  } catch (error) {
    if (!bunUnavailable(error)) throw error;
    await run('npx', ['--yes', 'bun', 'build', entryPath, '--target=bun', '--format=esm', `--outfile=${bundlePath}`]);
    await run('npx', ['--yes', 'bun', bundlePath]);
  }
} finally {
  await rm(tempDir, { recursive: true, force: true });
}

async function run(command, args) {
  const result = await execFileAsync(command, args, { cwd: resolvedSourceDir, maxBuffer: 16 * 1024 * 1024 });
  if (result.stdout) process.stdout.write(result.stdout);
  if (result.stderr) process.stderr.write(result.stderr);
}
function bunUnavailable(error) { return error && typeof error === 'object' && 'code' in error && error.code === 'ENOENT'; }
