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
	let pendingSessions: PersistedUploadSession[] = [];
	let accounts: AccountSummary[] = [];
	let selectedPendingUploadId = '';

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
		<form on:submit|preventDefault={uploadFile} class="upload-form"><label class="drop-zone" on:dragover|preventDefault on:drop={chooseDroppedFile}>Drop a file here or choose one<input type="file" on:change={chooseFile} /></label>{#if selectedFile}<p class="muted">Selected: {selectedFile.name} ({selectedFile.size} bytes)</p>{/if}<button type="submit" disabled={!selectedFile || upload?.state === 'UPLOADING'}>Upload</button></form>
		{#if upload}<p class="muted">Upload: {upload.state} · {upload.completedChunks}/{upload.totalChunks}{upload.retrying ? ` · retry ${upload.retryAttempt}` : ''}{#if upload.speedBytesPerSecond > 0} · {(upload.speedBytesPerSecond / 1024 / 1024).toFixed(1)} MiB/s{#if upload.etaSeconds !== undefined} · ETA {upload.etaSeconds}s{/if}{/if}</p>{/if}
		{#if pendingSessions.length > 0}<aside><strong>Pending upload metadata</strong><ul>{#each pendingSessions as session (session.uploadId)}<li>{session.name} · {session.completedIndexes.length}/{session.chunkCount} chunks<button type="button" on:click={() => selectedPendingUploadId = session.uploadId}>{selectedPendingUploadId === session.uploadId ? 'Selected' : 'Use this session'}</button><button type="button" on:click={() => void deleteUploadSession(session.uploadId).then(() => { pendingSessions = pendingSessions.filter((item) => item.uploadId !== session.uploadId); if (selectedPendingUploadId === session.uploadId) selectedPendingUploadId = ''; })}>Remove</button></li>{/each}</ul><p class="muted">Select the matching original file, then press Upload to resume the selected session.</p></aside>{/if}
		{#if queue && upload?.state === 'UPLOADING'}<button type="button" on:click={() => queue?.pause()}>Pause</button>{/if}
		{#if queue && upload?.state === 'PAUSED'}<button type="button" on:click={() => queue?.resume()}>Resume</button>{/if}
		{#if queue && ['UPLOADING', 'PAUSED'].includes(upload?.state ?? '')}<button type="button" on:click={() => void cancelUpload()}>Cancel upload</button>{/if}
	<section aria-live="polite">
		<nav aria-label="Directory path" class="breadcrumbs">
			<button type="button" on:click={() => { path = []; void loadDirectory(null); }}>Root</button>
			{#each path as directory, index (directory.id)}
				<span aria-hidden="true">/</span>
				<button type="button" on:click={() => { path = path.slice(0, index + 1); void loadDirectory(directory.id); }}>{directory.name}</button>
			{/each}
		</nav>
		<h2>Files</h2>
		<h2>Storage</h2><p class="muted">{accounts.reduce((sum, account) => sum + account.usedBytes, 0)} / {accounts.reduce((sum, account) => sum + account.totalBytes, 0)} bytes used</p>
		{#if loading}
			<p class="muted">Loading…</p>
		{:else if error}
			<p class="error">{error}</p>
		{:else}
			{#if path.length > 0}<button type="button" on:click={() => void goUp()}>← Up</button>{/if}
			{#if directories.length > 0}
				<h3>Directories</h3>
				<ul>{#each directories as directory (directory.id)}<li><button type="button" on:click={() => void enterDirectory(directory)}>📁 {directory.name}</button></li>{/each}</ul>
			{/if}
			{#if files.filter((file) => file.directoryId === currentDirectory).length === 0 && directories.length === 0}
				<p class="muted">No files yet.</p>
			{:else}
				<ul>
					{#each files.filter((file) => file.directoryId === currentDirectory) as file (file.id)}
						<li><span>{file.name}</span><span class="muted">{file.state} · {file.sizeBytes} bytes {#if file.state === 'AVAILABLE'}<a href={api.downloadUrl(file.id)} download={file.name}>Download</a>{/if} {#if file.state === 'AVAILABLE' || file.state === 'DEGRADED'}<button type="button" on:click={() => void deleteFile(file)}>Delete</button>{/if}</span></li>
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
	ul { padding: 0; list-style: none; }
	li { display: flex; justify-content: space-between; gap: 1rem; padding: 0.75rem 0; border-bottom: 1px solid #2a3040; }
</style>
