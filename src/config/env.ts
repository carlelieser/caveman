import { config } from 'dotenv';

/**
 * Loads a `.env` file into `process.env` for local runs. Importing this module
 * for its side effect is enough; import it before anything that reads env so the
 * values are present by the time module-level reads run. Real environment
 * variables already set are left untouched.
 */
config();
