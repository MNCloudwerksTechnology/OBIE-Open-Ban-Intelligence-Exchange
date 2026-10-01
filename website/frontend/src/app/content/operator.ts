// Facts about the operator and the deployment that the legal pages of every
// language state. The company data was supplied by the operator (WP #1677).

/** Operator's contact address for the Impressum and for privacy requests. */
export const OPERATOR_EMAIL = 'markus.niewerth@cloudwerks.de';

export const OPERATOR_PHONE = '+49 176 70526593';

export const OPERATOR_PHONE_HREF = 'tel:+4917670526593';

/** Street and town; each language adds the country in its own words. */
export const OPERATOR_ADDRESS: readonly string[] = ['Pottenort 15', '45891 Gelsenkirchen'];

/**
 * How long inquiries are kept, in months: the default of
 * `OBIE_INQUIRY_RETENTION` (`P12M`). Change it when the deployment sets
 * another value.
 */
export const INQUIRY_RETENTION_MONTHS = 12;

/** How long the web server keeps its access logs, in days (operator's setting). */
export const SERVER_LOG_RETENTION_DAYS = 7;

/**
 * Longest time an answered inquiry stays in the mailbox after the last
 * contact, in years, unless an agreement results: the rule of
 * https://cloudwerks.de/datenschutz ("Speicherdauer").
 */
export const INQUIRY_MAILBOX_RETENTION_YEARS = 3;

/**
 * How long Matomo keeps raw visit data, in months: the setting "Regularly
 * delete old raw data" for site 5 (operator's setting).
 */
export const ANALYTICS_RAW_DATA_RETENTION_MONTHS = 14;
