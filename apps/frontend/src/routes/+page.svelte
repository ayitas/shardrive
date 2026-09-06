<script lang="ts">
	import { onMount } from 'svelte';
	import { env } from '$env/dynamic/public';
	import { ShardriveApi, type AccountSummary, type DirectorySummary, type FileSummary } from '$lib/api/client';
	import { UploadQueue, type UploadSnapshot } from '$lib/upload/queue';
	import { deleteUploadSession, listUploadSessions, saveUploadSession, type PersistedUploadSession } from '$lib/upload/session-store';

	const api = new ShardriveApi();
	const apiBase = env.PUBLIC_API_BASE_URL || 'same origin';
	let files: FileSummary[] = [];
	let directories: DirectorySummary[] = [];
	let currentDirectory: string | null = null;
	let path: DirectorySummary[] = [];
	let loading = true;
	let error = '';
	let authenticated = false;
	let email = '';
	let password = '';
	let submitting = false;
	let selectedFile: File | null = null;
	let upload: UploadSnapshot | null = null;
	let queue: UploadQueue | null = null;
	let activeUploadName = '';
	let pendingSessions: PersistedUploadSession[] = [];
	let storageUsed = 0;
	let storageTotal = 0;
	let accounts: AccountSummary[] = [];
	let selectedPendingUploadId = '';
	let newFolderName = '';
	let creatingFolder = false;
	type SortOption = 'name-asc' | 'name-desc' | 'size-desc' | 'updated-desc';
	let sortBy: SortOption = 'updated-desc';
	let searchQuery = '';

	function formatBytes(bytes: number): string {
		if (bytes === 0) return '0 B';
		const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
		const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
		return `${(bytes / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
	}
	function uploadPercent(snapshot: UploadSnapshot): number {
		return snapshot.totalBytes > 0 ? Math.min(100, (snapshot.receivedBytes / snapshot.totalBytes) * 100) : 0;
	}
	function storagePercent(used: number, total: number): number { return total > 0 ? Math.min(100, (used / total) * 100) : 0; }
	function compareText(left: string, right: string): number { return left.localeCompare(right, undefined, { numeric: true, sensitivity: 'base' }); }
	function sortedDirectories(): DirectorySummary[] {
		return [...directories].sort((left, right) => {
			if (sortBy === 'name-desc') return compareText(right.name, left.name);
			if (sortBy === 'updated-desc') return right.updatedAt.localeCompare(left.updatedAt) || compareText(left.name, right.name);
			return compareText(left.name, right.name);
		});
	}
	function currentFiles(): FileSummary[] {
		const query = searchQuery.trim().toLocaleLowerCase();
		return files.filter((file) => file.directoryId === currentDirectory && (!query || file.name.toLocaleLowerCase().includes(query)));
	}
	function sortedFiles(): FileSummary[] {
		return [...currentFiles()].sort((left, right) => {
			if (sortBy === 'size-desc') return right.sizeBytes - left.sizeBytes || compareText(left.name, right.name);
			if (sortBy === 'name-desc') return compareText(right.name, left.name);
			if (sortBy === 'updated-desc') return right.updatedAt.localeCompare(left.updatedAt) || compareText(left.name, right.name);
			return compareText(left.name, right.name);
		});
	}

	async function loadDirectory(parentId: string | null): Promise<void> {
		loading = true;
		error = '';
		try {
			const [fileResponse, directoryResponse, accountResponse] = await Promise.all([
				api.listFiles(), api.listDirectories(parentId ?? undefined), api.listAccounts()
			]);
			files = fileResponse.files;
			directories = directoryResponse.directories;
			accounts = accountResponse.accounts;
			storageUsed = accounts.reduce((sum, account) => sum + account.usedBytes, 0);
			storageTotal = accounts.reduce((sum, account) => sum + account.totalBytes, 0);
			currentDirectory = parentId;
		} catch (cause) {
			error = cause instanceof Error ? cause.message : 'Could not load files';
		} finally {
			loading = false;
		}
	}

	async function enterDirectory(directory: DirectorySummary): Promise<void> {
		path = [...path, directory];
		await loadDirectory(directory.id);
	}

	async function goUp(): Promise<void> {
		path = path.slice(0, -1);
		await loadDirectory(path.at(-1)?.id ?? null);
	}

	async function createFolder(): Promise<void> {
		const name = newFolderName.trim();
		if (!name || creatingFolder) return;
		creatingFolder = true;
		error = '';
		try {
			await api.createDirectory(name, currentDirectory ?? undefined);
			newFolderName = '';
			await loadDirectory(currentDirectory);
		} catch (cause) {
			error = cause instanceof Error ? cause.message : 'Could not create folder';
		} finally {
			creatingFolder = false;
		}
	}

	onMount(async () => {
		try {
			pendingSessions = await reconcilePendingSessions(await listUploadSessions());
			await api.me();
			authenticated = true;
			await loadDirectory(null);
		} catch {
			loading = false;
		}
	});

	async function reconcilePendingSessions(sessions: PersistedUploadSession[]): Promise<PersistedUploadSession[]> {
		const active: PersistedUploadSession[] = [];
		await Promise.all(sessions.map(async (session) => {
			try {
				const status = await api.getUpload(session.uploadId);
				if (status.state === 'COMPLETED' || status.state === 'CANCELLED' || status.state === 'FAILED' || status.state === 'EXPIRED') {
					await deleteUploadSession(session.uploadId);
					return;
				}
				const refreshed = { ...session, completedIndexes: status.completedIndexes, updatedAt: new Date().toISOString() };
				active.push(refreshed);
				await saveUploadSession(refreshed);
			} catch {
				active.push(session);
			}
		}));
		return active.sort((a, b) => b.updatedAt.localeCompare(a.updatedAt));
	}

	async function signIn(): Promise<void> {
		submitting = true;
		error = '';
		try {
			await api.login(email, password);
			password = '';
			authenticated = true;
			await loadDirectory(null);
		} catch (cause) {
			error = cause instanceof Error ? cause.message : 'Could not sign in';
		} finally {
			submitting = false;
		}
	}

	async function signOut(): Promise<void> {
		await api.logout();
		authenticated = false;
		files = [];
		directories = [];
		queue = null;
		upload = null;
		activeUploadName = '';
	}

	function chooseFile(event: Event): void { selectedFile = (event.currentTarget as HTMLInputElement).files?.[0] ?? null; }
	function chooseDroppedFile(event: DragEvent): void {
		event.preventDefault();
		selectedFile = event.dataTransfer?.files?.[0] ?? null;
	}

	async function uploadFile(): Promise<void> {
		if (!selectedFile) return;
		error = '';
		const file = selectedFile;
		activeUploadName = file.name;
		const pending = selectedPendingUploadId ? pendingSessions.find((session) => session.uploadId === selectedPendingUploadId) : undefined;
		if (pending && (pending.name !== file.name || pending.size !== file.size)) { error = 'Selected file does not match the pending upload.'; return; }
		queue = new UploadQueue(api, file, { name: file.name, size: file.size, mimeType: file.type || undefined, directoryId: currentDirectory ?? undefined }, { resume: pending, concurrency: 3, onChange: (state: UploadSnapshot) => { upload = state; if (state.uploadId && state.fileId) void saveUploadSession({ uploadId: state.uploadId, fileId: state.fileId, name: file.name, size: file.size, chunkSize: state.chunkSize, chunkCount: state.totalChunks, completedIndexes: state.completedIndexes, updatedAt: new Date().toISOString() }); } });
		try { await queue.start(); const completedUploadId = queue.getSnapshot().uploadId; if (completedUploadId) { await deleteUploadSession(completedUploadId); pendingSessions = pendingSessions.filter((session) => session.uploadId !== completedUploadId); } selectedFile = null; selectedPendingUploadId = ''; await loadDirectory(currentDirectory); } catch (cause) { if (queue.getSnapshot().state !== 'CANCELLED') error = cause instanceof Error ? cause.message : 'Upload failed'; }
	}

	async function cancelUpload(): Promise<void> {
		if (!queue) return;
		try {
			await queue.cancel();
			const uploadId = queue.getSnapshot().uploadId;
			if (uploadId) {
				await deleteUploadSession(uploadId);
				pendingSessions = pendingSessions.filter((session) => session.uploadId !== uploadId);
			}
		} catch (cause) { error = cause instanceof Error ? cause.message : 'Could not cancel upload'; }
	}

	async function deleteFile(file: FileSummary): Promise<void> {
		if (!confirm(`Delete ${file.name}?`)) return;
		try { await api.deleteFile(file.id); await loadDirectory(currentDirectory); }
		catch (cause) { error = cause instanceof Error ? cause.message : 'Could not delete file'; }
	}

	async function renameFile(file: FileSummary): Promise<void> {
		const name = window.prompt('New file name', file.name)?.trim();
		if (!name || name === file.name) return;
		try { await api.renameFile(file.id, name); await loadDirectory(currentDirectory); }
		catch (cause) { error = cause instanceof Error ? cause.message : 'Could not rename file'; }
	}

	async function renameDirectory(directory: DirectorySummary): Promise<void> {
		const name = window.prompt('New folder name', directory.name)?.trim();
		if (!name || name === directory.name) return;
		try { await api.renameDirectory(directory.id, name); await loadDirectory(currentDirectory); }
		catch (cause) { error = cause instanceof Error ? cause.message : 'Could not rename folder'; }
	}

	async function deleteDirectory(directory: DirectorySummary): Promise<void> {
		if (!window.confirm(`Delete folder ${directory.name}? It must be empty.`)) return;
		try { await api.deleteDirectory(directory.id); await loadDirectory(currentDirectory); }
		catch (cause) { error = cause instanceof Error ? cause.message : 'Could not delete folder'; }
	}
</script>

<svelte:head>
	<title>Shardrive</title>
	<meta name="description" content="Self-hosted virtual storage pool" />
</svelte:head>

	<main>
	<h1>Shardrive</h1>
	<p>Phase 1 frontend is ready.</p>
	<p class="muted">API: {apiBase}</p>
	{#if !authenticated}
		<form on:submit|preventDefault={signIn} aria-label="Sign in">
			<label>Email <input type="email" bind:value={email} autocomplete="username" required /></label>
			<label>Password <input type="password" bind:value={password} autocomplete="current-password" required /></label>
			<button type="submit" disabled={submitting}>{submitting ? 'Signing in…' : 'Sign in'}</button>
		</form>
		{#if error}<p class="error" role="alert">{error}</p>{/if}
	{:else}
		<button type="button" on:click={() => void signOut()}>Sign out</button>
		<form on:submit|preventDefault={uploadFile} class="upload-form"><label class="drop-zone" on:dragover|preventDefault on:drop={chooseDroppedFile}>Drop a file here or choose one<input type="file" on:change={chooseFile} /></label>{#if selectedFile}<p class="muted">Selected: {selectedFile.name} ({formatBytes(selectedFile.size)})</p>{/if}<button type="submit" disabled={!selectedFile || upload?.state === 'UPLOADING'}>Upload</button></form>
		{#if upload}
			<section class="upload-card" aria-label="Active upload">
				<div class="upload-heading"><div><strong>{activeUploadName}</strong><span class="status-pill">{upload.state}</span></div><span class="muted">{formatBytes(upload.receivedBytes)} / {formatBytes(upload.totalBytes)}</span></div>
				<div class="meter" role="progressbar" aria-label="Upload progress" aria-valuenow={upload.receivedBytes} aria-valuemin="0" aria-valuemax={upload.totalBytes}><span style={`width: ${uploadPercent(upload)}%`}></span></div>
				<div class="upload-details"><span>{upload.completedChunks}/{upload.totalChunks} chunks</span>{#if upload.speedBytesPerSecond > 0}<span>{(upload.speedBytesPerSecond / 1024 / 1024).toFixed(1)} MiB/s{#if upload.etaSeconds !== undefined} · ETA {upload.etaSeconds}s{/if}</span>{/if}{#if upload.retrying}<span>Retry {upload.retryAttempt}</span>{/if}</div>
				{#if upload.error}<p class="error">{upload.error}</p>{/if}
				<div class="upload-actions">{#if queue && upload.state === 'UPLOADING'}<button type="button" on:click={() => queue?.pause()}>Pause</button>{/if}{#if queue && upload.state === 'PAUSED'}<button type="button" on:click={() => queue?.resume()}>Resume</button>{/if}{#if queue && ['UPLOADING', 'PAUSED'].includes(upload.state)}<button type="button" on:click={() => void cancelUpload()}>Cancel upload</button>{/if}</div>
			</section>
		{/if}
		{#if pendingSessions.length > 0}<aside class="pending-card"><div class="section-heading"><strong>Pending uploads</strong><span class="muted">{pendingSessions.length}</span></div><ul>{#each pendingSessions as session (session.uploadId)}<li><div><strong>{session.name}</strong><span class="muted">{formatBytes(session.size)} · {session.completedIndexes.length}/{session.chunkCount} chunks</span></div><div class="upload-actions"><button type="button" on:click={() => selectedPendingUploadId = session.uploadId}>{selectedPendingUploadId === session.uploadId ? 'Selected' : 'Resume'}</button><button type="button" on:click={() => void deleteUploadSession(session.uploadId).then(() => { pendingSessions = pendingSessions.filter((item) => item.uploadId !== session.uploadId); if (selectedPendingUploadId === session.uploadId) selectedPendingUploadId = ''; })}>Remove</button></div></li>{/each}</ul><p class="muted">Choose the matching original file, select Resume, then press Upload.</p></aside>{/if}
		<section aria-live="polite">
		<nav aria-label="Directory path" class="breadcrumbs">
			<button type="button" on:click={() => { path = []; void loadDirectory(null); }}>Root</button>
			{#each path as directory, index (directory.id)}
				<span aria-hidden="true">/</span>
				<button type="button" on:click={() => { path = path.slice(0, index + 1); void loadDirectory(directory.id); }}>{directory.name}</button>
			{/each}
		</nav>
		<div class="content-header">
			<div>
				<h2>Files</h2>
				<p class="muted">{currentFiles().length} files · {directories.length} folders</p>
			</div>
			<div class="content-actions">
				<label class="search-control"><span class="sr-only">Search files</span><input aria-label="Search files" bind:value={searchQuery} placeholder="Search files" /></label>
				<label class="sort-control">Sort by <select aria-label="Sort files and folders" bind:value={sortBy}><option value="updated-desc">Recently updated</option><option value="name-asc">Name A–Z</option><option value="name-desc">Name Z–A</option><option value="size-desc">Size largest</option></select></label>
				<form class="new-folder-form" on:submit|preventDefault={createFolder}>
					<label class="sr-only" for="new-folder-name">New folder name</label>
					<input id="new-folder-name" aria-label="New folder name" bind:value={newFolderName} placeholder="New folder name" maxlength="255" />
					<button type="submit" disabled={!newFolderName.trim() || creatingFolder}>{creatingFolder ? 'Creating…' : 'New folder'}</button>
				</form>
			</div>
		</div>
		<div class="storage-card">
			<div class="storage-heading"><span>Storage</span><strong>{formatBytes(storageUsed)} / {formatBytes(storageTotal)}</strong></div>
			<div class="meter" role="progressbar" aria-label="Storage used" aria-valuenow={storageUsed} aria-valuemin="0" aria-valuemax={storageTotal}><span style={`width: ${storagePercent(storageUsed, storageTotal)}%`}></span></div>
			<p class="muted">{storageTotal > 0 ? `${formatBytes(Math.max(0, storageTotal - storageUsed))} available` : 'Storage is not configured'}</p>
		</div>
		<section class="accounts-card" aria-label="Storage accounts">
			<div class="section-heading"><h2>Storage accounts</h2><span class="muted">{accounts.length} connected</span></div>
			{#if accounts.length === 0}
				<p class="muted">No storage accounts are configured.</p>
			{:else}
				<ul class="account-list">
					{#each accounts as account (account.id)}
						<li>
							<div class="account-name"><strong>{account.name}</strong><span class="muted">{account.provider}</span></div>
							<div class="account-summary"><span class={`status-pill status-${account.state.toLowerCase()}`}>{account.state}</span><span class="muted">{formatBytes(account.usedBytes)} / {formatBytes(account.totalBytes)}</span></div>
							<div class="meter" role="progressbar" aria-label={`${account.name} storage used`} aria-valuenow={account.usedBytes} aria-valuemin="0" aria-valuemax={account.totalBytes}><span style={`width: ${storagePercent(account.usedBytes, account.totalBytes)}%`}></span></div>
							{#if account.state !== 'ACTIVE'}<p class="account-warning">This account is not receiving new chunks until it is healthy.</p>{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</section>
		{#if loading}
			<p class="muted">Loading…</p>
		{:else if error}
			<p class="error">{error}</p>
		{:else}
			{#if path.length > 0}<button type="button" on:click={() => void goUp()}>← Up</button>{/if}
			{#if sortedDirectories().length > 0}
				<h3>Directories</h3>
				<ul>{#each sortedDirectories() as directory (directory.id)}<li><button type="button" on:click={() => void enterDirectory(directory)}>📁 {directory.name}</button><span class="row-actions"><button type="button" on:click={() => void renameDirectory(directory)}>Rename</button><button type="button" on:click={() => void deleteDirectory(directory)}>Delete</button></span></li>{/each}</ul>
			{/if}
			{#if currentFiles().length === 0 && sortedDirectories().length === 0}
				<p class="muted">No files yet.</p>
			{:else}
				<ul>
					{#each sortedFiles() as file (file.id)}
						<li><span>{file.name}</span><span class="muted">{file.state} · {file.sizeBytes} bytes {#if file.state === 'AVAILABLE'}<a href={api.downloadUrl(file.id)} download={file.name}>Download</a>{/if} {#if file.state === 'AVAILABLE' || file.state === 'DEGRADED'}<button type="button" on:click={() => void renameFile(file)}>Rename</button><button type="button" on:click={() => void deleteFile(file)}>Delete</button>{/if}</span></li>
				{/each}
			</ul>
			{/if}
		{/if}
	</section>
	{/if}
</main>

<style>
	:global(body) {
		margin: 0;
		font-family: system-ui, sans-serif;
		background: #10131a;
		color: #eef2ff;
	}

	main {
		max-width: 56rem;
		margin: 0 auto;
		padding: 5rem 1.5rem;
	}

	.content-header, .storage-heading { display: flex; justify-content: space-between; gap: 1rem; align-items: center; }
	.content-header { margin-top: 2rem; }
	.content-header h2 { margin-bottom: .2rem; }
	.content-actions { display: flex; gap: .75rem; align-items: center; }
	.search-control input { width: 10rem; }
	.sort-control { display: flex; gap: .4rem; align-items: center; white-space: nowrap; color: #aab2c5; font-size: .85rem; }
	select { padding: .55rem; border-radius: .35rem; border: 1px solid #3b465d; background: #171c27; color: inherit; }
	.new-folder-form { display: flex; gap: .5rem; align-items: center; margin: 0; max-width: none; }
	.new-folder-form input { width: 11rem; }
	.upload-card, .pending-card { margin: 1rem 0; padding: 1rem; border: 1px solid #2f3a50; border-radius: .75rem; background: #151b27; }
	.upload-heading, .upload-details, .section-heading { display: flex; justify-content: space-between; gap: 1rem; align-items: center; }
	.upload-heading > div { display: flex; gap: .6rem; align-items: center; min-width: 0; }
	.upload-heading strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
	.status-pill { padding: .15rem .45rem; border-radius: 999px; background: #293349; color: #b9c7e6; font-size: .75rem; }
	.upload-details { color: #aab2c5; font-size: .85rem; }
	.upload-actions { display: flex; gap: .5rem; justify-content: flex-end; margin-top: .8rem; }
	.row-actions { display: flex; gap: .5rem; }
	.pending-card ul { margin-bottom: .5rem; }
	.pending-card li { align-items: center; }
	.pending-card li > div:first-child { display: grid; gap: .2rem; min-width: 0; }
	.storage-card { margin: 1rem 0 2rem; padding: 1rem; border: 1px solid #2f3a50; border-radius: .75rem; background: #151b27; }
	.accounts-card { margin: 1rem 0 2rem; padding: 1rem; border: 1px solid #2f3a50; border-radius: .75rem; background: #151b27; }
	.storage-heading strong { font-size: .9rem; }
	.account-list { margin: .5rem 0 0; }
	.account-list li { display: block; }
	.account-name, .account-summary { display: flex; justify-content: space-between; gap: 1rem; align-items: center; }
	.account-name { justify-content: flex-start; }
	.account-name .muted { font-size: .85rem; }
	.account-summary { margin-top: .35rem; font-size: .85rem; }
	.account-warning { margin: .5rem 0 0; color: #f4c27a; font-size: .85rem; }
	.status-active { background: #164e3b; color: #a7f3d0; }
	.status-full, .status-rate_limited { background: #713f12; color: #fde68a; }
	.status-offline, .status-auth_failed, .status-auth_required, .status-disabled { background: #5b1d2a; color: #fecdd3; }
	.meter { height: .5rem; margin: .8rem 0 .5rem; overflow: hidden; border-radius: 999px; background: #293349; }
	.meter span { display: block; height: 100%; border-radius: inherit; background: linear-gradient(90deg, #6ee7b7, #60a5fa); transition: width .2s ease; }
	.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }

	.muted {
		color: #aab2c5;
	}

	.error { color: #ff9d9d; }
	.breadcrumbs { display: flex; gap: .5rem; align-items: center; margin-bottom: 1rem; }
	button { color: inherit; background: #242b3a; border: 1px solid #3b465d; border-radius: .35rem; padding: .35rem .6rem; cursor: pointer; }
	form { display: grid; gap: .8rem; max-width: 24rem; margin: 2rem 0; }
	label { display: grid; gap: .3rem; }
	.drop-zone { border: 1px dashed #687697; border-radius: .5rem; padding: 1rem; text-align: center; cursor: copy; }
	input { padding: .55rem; border-radius: .35rem; border: 1px solid #3b465d; background: #171c27; color: inherit; }
	button:disabled { cursor: not-allowed; opacity: .55; }
	ul { padding: 0; list-style: none; }
	li { display: flex; justify-content: space-between; gap: 1rem; padding: 0.75rem 0; border-bottom: 1px solid #2a3040; }

	@media (max-width: 640px) {
		main { padding: 2rem 1rem; }
		.content-header { align-items: stretch; }
		.content-actions { flex-direction: column; align-items: stretch; }
		.search-control input { box-sizing: border-box; width: 100%; }
		.sort-control { justify-content: space-between; }
		.sort-control select { flex: 1; min-width: 0; }
		.new-folder-form { width: 100%; }
		.new-folder-form input { flex: 1; min-width: 0; width: auto; }
		.upload-heading, .upload-details { align-items: flex-start; flex-wrap: wrap; }
		.upload-heading > div { max-width: 100%; }
		.upload-actions { flex-wrap: wrap; }
		.breadcrumbs { flex-wrap: wrap; }
		li { align-items: stretch; flex-direction: column; }
		li > .muted, .row-actions { display: flex; flex-wrap: wrap; gap: .5rem; align-items: center; }
	}
</style>
