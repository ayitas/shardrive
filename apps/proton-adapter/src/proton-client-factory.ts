/**
 * The SDK constructor is injected because the published SDK must be bundled
 * before it can run in the adapter's Node process.
 */
export interface ProtonSDKRuntime<
	TClient = unknown,
	TParameters extends object = Record<string, unknown>
> {
	ProtonDriveClient: new (parameters: TParameters) => TClient;
}

export interface ProtonSessionProvider<
	TParameters extends object = Record<string, unknown>
> {
	load(accountId: string): Promise<TParameters | undefined>;
}

export class ProtonAuthRequiredError extends Error {
	readonly accountId: string;

	constructor(accountId: string) {
		super(`Proton session is required for account ${accountId}`);
		this.name = 'ProtonAuthRequiredError';
		this.accountId = accountId;
	}
}

export function createProtonDriveClient<
	TClient,
	TParameters extends object
>(runtime: ProtonSDKRuntime<TClient, TParameters>, parameters: TParameters): TClient {
	return new runtime.ProtonDriveClient(parameters);
}

export async function createProtonDriveClientForAccount<
	TClient,
	TParameters extends object
>(
	runtime: ProtonSDKRuntime<TClient, TParameters>,
	sessions: ProtonSessionProvider<TParameters>,
	accountId: string
): Promise<TClient> {
	const parameters = await sessions.load(accountId);
	if (!parameters) throw new ProtonAuthRequiredError(accountId);
	return createProtonDriveClient(runtime, parameters);
}
