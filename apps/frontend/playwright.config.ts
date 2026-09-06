import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
	testDir: './e2e',
	testMatch: '**/*.spec.ts',
	fullyParallel: false,
	forbidOnly: Boolean(process.env.CI),
	retries: process.env.CI ? 2 : 0,
	reporter: process.env.CI ? 'github' : 'list',
	use: { baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:3000', trace: 'retain-on-failure' },
	projects: [
		{ name: 'firefox', use: { ...devices['Desktop Firefox'] } },
		{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }
	],
	timeout: 120_000,
});
