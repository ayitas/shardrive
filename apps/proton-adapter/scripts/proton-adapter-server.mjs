import { execFile, execFileSync, spawn } from 'node:child_process';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);
const require = createRequire(import.meta.url);
const sourceDir = process.env.PROTON_SDK_SOURCE_DIR;
const expectedCommit = process.env.PROTON_SDK_EXPECTED_COMMIT ?? 'c8d03244938a6b4d107c755df8904d7d971ed1c2';
const masterKey = process.env.SHARDRIVE_PROTON_MASTER_KEY_B64;
const sessionRoot = process.env.SHARDRIVE_PROTON_SESSION_ROOT;
const grpcAddress = process.env.SHARDRIVE_PROTON_GRPC_ADDRESS ?? '0.0.0.0:50051';
const protoIncludeDir = path.dirname(require.resolve('google-proto-files/google/protobuf/empty.proto'));

if (!sourceDir) throw new Error('PROTON_SDK_SOURCE_DIR must point to the official Proton SDK source tree');
if (!masterKey) throw new Error('SHARDRIVE_PROTON_MASTER_KEY_B64 is required');
if (!sessionRoot) throw new Error('SHARDRIVE_PROTON_SESSION_ROOT is required');
const resolvedSourceDir = path.resolve(sourceDir);
const actualCommit = execFileSync('git', ['-C', resolvedSourceDir, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
if (actualCommit !== expectedCommit) throw new Error(`official Proton SDK commit mismatch: expected ${expectedCommit}, got ${actualCommit}`);

const tempDir = await mkdtemp(path.join(resolvedSourceDir, 'cli', '.shardrive-adapter-'));
const runtimeDir = await mkdtemp(path.join(path.dirname(fileURLToPath(import.meta.url)), '.runtime-'));
const entryPath = path.join(tempDir, 'entry.ts');
const bundlePath = path.join(runtimeDir, 'bundle.mjs');
const sdkIndex = JSON.stringify(`${resolvedSourceDir}/client/js/src/index.ts`);
const accountIndex = JSON.stringify(`${resolvedSourceDir}/incubating/account/js/src/index.ts`);
const sourceRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../src');
const protoPath = path.resolve(sourceRoot, '../../../proto/storage.proto');
process.env.SHARDRIVE_PROTO_INCLUDE_DIR = protoIncludeDir;
process.env.SHARDRIVE_PROTO_PATH = protoPath;
const grpcServer = JSON.stringify(`${sourceRoot}/grpc-server.ts`);
const backendSource = JSON.stringify(`${sourceRoot}/proton-storage-backend.ts`);
const sessionVault = JSON.stringify(`${sourceRoot}/session-vault.ts`);
const clientFactory = JSON.stringify(`${sourceRoot}/proton-client-factory.ts`);

const entry = `
process.env.SHARDRIVE_PROTO_INCLUDE_DIR = ${JSON.stringify(protoIncludeDir)};
process.env.SHARDRIVE_PROTO_PATH = ${JSON.stringify(protoPath)};
console.error('loading Proton adapter runtime');
const { CryptoProxy } = await import('@protontech/crypto');
const { Api: CryptoApi } = await import('@protontech/crypto/proxy/endpoint/api.ts');
const { MemoryCache, NullFeatureFlagProvider, OpenPGPCryptoWithCryptoProxy, ProtonDriveClient } = await import(${sdkIndex});
const { Telemetry } = await import(${JSON.stringify(`${resolvedSourceDir}/client/js/src/telemetry.ts`)});
const { ApiClient, initAccount } = await import(${accountIndex});
const { startStorageAdapter } = await import(${grpcServer});
const { ProtonStorageBackend } = await import(${backendSource});
const { FileSessionStore, SessionVault } = await import(${sessionVault});
const { ProtonAuthRequiredError } = await import(${clientFactory});
console.error('Proton adapter runtime loaded');

class Credentials {
  private callbacks = new Set<() => void>();
  constructor(private snapshot: { userKeyPassword: string; session: { uid: string; accessToken: string; refreshToken?: string }; telemetryEnabled?: boolean }) {}
  get uid() { return this.snapshot.session.uid; }
  get accessToken() { return this.snapshot.session.accessToken; }
  get refreshToken() { return this.snapshot.session.refreshToken; }
  on(_: 'sessionInfoChanged', callback: () => void) { this.callbacks.add(callback); }
  isLoggedIn() { return true; }
  isTelemetryEnabled() { return this.snapshot.telemetryEnabled ?? false; }
  getUserKeyPassword() { return this.snapshot.userKeyPassword; }
  async load() {}
  async setUserKeyPassword(value: string) { this.snapshot.userKeyPassword = value; this.callbacks.forEach((callback) => callback()); }
  async setSessionInfo(value: { uid: string; accessToken: string; refreshToken?: string }) { this.snapshot.session = value; this.callbacks.forEach((callback) => callback()); }
  async setTelemetryEnabled(value: boolean) { this.snapshot.telemetryEnabled = value; }
  async signOut() {}
}

const decodedKey = Buffer.from(process.env.SHARDRIVE_PROTON_MASTER_KEY_B64 ?? '', 'base64');
if (decodedKey.byteLength !== 32) throw new Error('SHARDRIVE_PROTON_MASTER_KEY_B64 must decode to 32 bytes');
const sessions = new FileSessionStore(process.env.SHARDRIVE_PROTON_SESSION_ROOT!, new SessionVault(decodedKey));
const runtimes = new Map<string, Promise<any>>();
const runtimeFor = (accountId: string) => {
  let runtime = runtimes.get(accountId);
  if (!runtime) { runtime = createRuntime(accountId); runtimes.set(accountId, runtime); }
  return runtime;
};

async function createRuntime(accountId: string) {
  const snapshot = await sessions.get(accountId);
  if (!snapshot) throw new ProtonAuthRequiredError(accountId);
  const credentials = new Credentials(snapshot as any);
  const logger = { debug() {}, info() {}, warn() {}, error() {} };
  const apiClient = new ApiClient({ baseUrl: 'drive-api.proton.me', appVersion: 'shardrive-proton-adapter', credentials, logger, headers: { 'x-pm-drive-sdk-version': '0.21.0' } });
  CryptoApi.init({}); CryptoProxy.setEndpoint(new CryptoApi(), (endpoint) => endpoint.clearKeyStore());
  const { addresses, srp, accountApi } = await initAccount({ authClientId: 'cli-drive', apiClient, credentials, cryptoProxy: CryptoProxy, logger });
  const telemetry = new Telemetry({ logHandlers: [], metricHandlers: [] });
  const client = new ProtonDriveClient({
    config: { baseUrl: 'drive-api.proton.me', clientUid: 'shardrive-proton-adapter' },
    httpClient: {
      fetchJson: (request) => apiClient.authenticatedRequest(request.url, { method: request.method, headers: request.headers, ...(request.json !== undefined ? { json: request.json } : {}), ...(request.body !== undefined && request.json === undefined ? { body: request.body } : {}), timeout: request.timeoutMs, signal: request.signal, throwHttpErrors: false }),
      fetchBlob: (request) => apiClient.authenticatedRequest(request.url, { method: request.method, headers: request.headers, body: request.body, timeout: request.timeoutMs, signal: request.signal, throwHttpErrors: false }),
    },
    entitiesCache: new MemoryCache(), cryptoCache: new MemoryCache(), telemetry, openPGPCryptoModule: new OpenPGPCryptoWithCryptoProxy(CryptoProxy),
    account: { getOwnPrimaryAddress: () => addresses.getOwnPrimaryAddress(), getOwnAddresses: () => addresses.getOwnAddresses(), getOwnAddress: (value: string) => addresses.getOwnAddress(value), hasProtonAccount: (value: string) => addresses.hasProtonAccount(value), getPublicKeys: (value: string, forceRefresh?: boolean) => addresses.getPublicKeys(value, forceRefresh) },
    srpModule: srp, featureFlagProvider: new NullFeatureFlagProvider(),
  });
  return {
    client,
    health: async () => { await client.getMyFilesRootFolder(); },
    usage: async () => {
      const users = await accountApi.users();
      const user = users.User;
      if (!user) throw new Error('Proton account response did not include user quota');
      return { totalBytes: user.MaxSpace, usedBytes: user.UsedSpace, freeBytes: Math.max(0, user.MaxSpace - user.UsedSpace) };
    }
  };
}

const adapter = await startStorageAdapter(new ProtonStorageBackend(runtimeFor), process.env.SHARDRIVE_PROTON_GRPC_ADDRESS ?? '0.0.0.0:50051');
console.error('Shardrive Proton adapter listening on ' + adapter.address);
const shutdown = async () => { await adapter.close(); process.exit(0); };
process.once('SIGINT', shutdown);
process.once('SIGTERM', shutdown);
`;

await writeFile(entryPath, entry, { mode: 0o600 });
try {
  const bun = process.env.PROTON_BUN_BIN ?? 'bun';
  try {
    await run(bun, ['build', entryPath, '--target=bun', '--format=esm', '--external:@grpc/grpc-js', '--external:@grpc/proto-loader', '--external:google-proto-files', `--outfile=${bundlePath}`]);
    await runStreaming(bun, [bundlePath]);
  } catch (error) {
    if (!bunUnavailable(error)) throw error;
    await run('npx', ['--yes', 'bun', 'build', entryPath, '--target=bun', '--format=esm', '--external:@grpc/grpc-js', '--external:@grpc/proto-loader', '--external:google-proto-files', `--outfile=${bundlePath}`]);
    await runStreaming('npx', ['--yes', 'bun', bundlePath]);
  }
} finally {
  await rm(tempDir, { recursive: true, force: true });
  await rm(runtimeDir, { recursive: true, force: true });
}

async function run(command, args) {
  const result = await execFileAsync(command, args, { cwd: resolvedSourceDir, maxBuffer: 16 * 1024 * 1024 });
  if (result.stdout) process.stdout.write(result.stdout);
  if (result.stderr) process.stderr.write(result.stderr);
}
function runStreaming(command, args) {
	return new Promise((resolve, reject) => {
		const child = spawn(command, args, { stdio: 'inherit' });
		child.once('error', reject);
		child.once('exit', (code, signal) => {
			if (code === 0) resolve();
			else reject(new Error(`${command} exited with ${signal ?? code}`));
		});
	});
}
function bunUnavailable(error) { return error && typeof error === 'object' && 'code' in error && error.code === 'ENOENT'; }
