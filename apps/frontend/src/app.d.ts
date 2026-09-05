declare global {
	namespace App {
		interface Error {
			message: string;
			code?: string;
		}

		interface Locals {
			// Authentication will populate the session in a later Phase 1 slice.
		}

		interface PageData {}
		interface Platform {}
	}
}

export {};
