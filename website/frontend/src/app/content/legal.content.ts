import { InjectionToken } from '@angular/core';

import { LegalContent } from './legal-content.model';
import { LINKS } from './landing.content';

// Copy of the Impressum (§ 5 DDG, § 18 MStV) and the privacy policy
// (Art. 13 GDPR). The company data was supplied by the operator (WP #1677);
// nothing here is invented. What is still unknown is marked TODO(operator)
// and listed in website/README.md ("Legal pages"). The privacy policy
// describes what the code does: change it together with the code.

/** Operator's contact address for the Impressum and for privacy requests. */
export const OPERATOR_EMAIL = 'markus.niewerth@cloudwerks.de';

const OPERATOR_PHONE = '+49 176 70526593';
const OPERATOR_ADDRESS = ['Pottenort 15', '45891 Gelsenkirchen', 'Germany'];

/**
 * How long inquiries are kept: the default of `OBIE_INQUIRY_RETENTION`
 * (`P12M`). Change it here when the deployment sets another value.
 */
export const INQUIRY_RETENTION = '12 months';

/** How long the web server keeps its access logs (operator's setting). */
export const SERVER_LOG_RETENTION = '7 days';

/** English legal pages. */
export const LEGAL_CONTENT_EN: LegalContent = {
  reviewPending: true,
  reviewNotice: {
    heading: 'Draft: pending the operator’s review',
    text: 'This page must be reviewed by the operator before the site goes live. Until then it is a draft and not legal advice.',
  },
  impressum: {
    meta: {
      title: 'Impressum (legal notice) · OBIE',
      description:
        'Legal notice of the OBIE website under § 5 DDG: provider, contact, register entry and VAT ID.',
    },
    heading: 'Legal notice',
    legalTerm: 'Impressum',
    sections: [
      {
        id: 'provider',
        heading: 'Information under § 5 DDG',
        legalTerm: 'Angaben gemäß § 5 DDG',
        blocks: [
          {
            kind: 'facts',
            items: [
              { term: 'Provider', lines: ['Cloudwerks Technology GmbH'] },
              { term: 'Address', lines: OPERATOR_ADDRESS },
              { term: 'Represented by', lines: ['Markus Niewerth, managing director'] },
            ],
          },
        ],
      },
      {
        id: 'contact',
        heading: 'Contact',
        legalTerm: 'Kontakt',
        blocks: [
          {
            kind: 'facts',
            items: [
              { term: 'Phone', lines: [OPERATOR_PHONE], href: 'tel:+4917670526593' },
              { term: 'E-mail', lines: [OPERATOR_EMAIL], href: `mailto:${OPERATOR_EMAIL}` },
            ],
          },
        ],
      },
      {
        id: 'register',
        heading: 'Register entry',
        legalTerm: 'Eintragung im Handelsregister',
        blocks: [
          {
            kind: 'facts',
            items: [
              { term: 'Register court', lines: ['Amtsgericht Gelsenkirchen'] },
              { term: 'Register number', lines: ['HRB 17839'] },
            ],
          },
        ],
      },
      {
        id: 'vat-id',
        heading: 'VAT identification number under § 27a UStG',
        legalTerm: 'Umsatzsteuer-Identifikationsnummer gemäß § 27a UStG',
        blocks: [{ kind: 'facts', items: [{ term: 'VAT ID', lines: ['DE363640900'] }] }],
      },
      {
        id: 'responsible',
        heading: 'Responsible for the content under § 18(2) MStV',
        legalTerm: 'Verantwortlich für den Inhalt nach § 18 Abs. 2 MStV',
        blocks: [
          {
            kind: 'facts',
            items: [
              {
                term: 'Responsible',
                lines: ['Markus Niewerth', ...OPERATOR_ADDRESS.slice(0, 2)],
              },
            ],
          },
        ],
      },
      {
        id: 'dispute-resolution',
        heading: 'Consumer dispute resolution',
        legalTerm: 'Verbraucherstreitbeilegung',
        blocks: [
          {
            kind: 'paragraph',
            text: 'We are neither willing nor obliged to take part in dispute resolution proceedings before a consumer arbitration board.',
          },
        ],
      },
      {
        id: 'liability-content',
        heading: 'Liability for content',
        legalTerm: 'Haftung für Inhalte',
        blocks: [
          {
            kind: 'paragraph',
            text: 'We create the content of this website with great care and to the best of our knowledge, but we cannot guarantee that it is correct, complete and up to date. As a service provider we are responsible for our own content on these pages under the general laws. We are not obliged to monitor third-party information that is transmitted or stored, or to look for circumstances that indicate illegal activity. Obligations to remove or block information under the general laws remain unaffected.',
          },
          {
            kind: 'paragraph',
            text: 'Such liability is only possible from the moment we learn of a specific infringement. As soon as we learn of one, we remove the content concerned without delay.',
          },
        ],
      },
      {
        id: 'liability-links',
        heading: 'Liability for links',
        legalTerm: 'Haftung für Links',
        blocks: [
          {
            kind: 'paragraph',
            text: 'This website links to external websites of third parties, for example GitHub. We have no influence on their content and therefore cannot accept any liability for it; the provider or operator of the linked site is always responsible for its content.',
          },
          {
            kind: 'paragraph',
            text: 'We checked the linked sites for possible legal violations when we linked them and found none. Permanent monitoring of linked content is not reasonable without concrete evidence of a violation. As soon as we learn of one, we remove the link without delay.',
          },
        ],
      },
      {
        id: 'copyright',
        heading: 'Copyright',
        legalTerm: 'Urheberrecht',
        blocks: [
          {
            kind: 'paragraph',
            text: 'The content and works on this website are subject to German copyright law. Unless a licence says otherwise, copying, editing, distributing or any other use beyond the limits of copyright law needs the prior written consent of the author. The OBIE source code and specification are published under the ',
            link: { label: 'MIT licence', href: LINKS.licence },
            after: ', which allows their use under its terms.',
          },
          {
            kind: 'paragraph',
            text: 'Where content on this site was not created by us, the copyrights of third parties are respected. If you nevertheless notice a copyright infringement, please let us know. As soon as we learn of one, we remove the content concerned without delay.',
          },
        ],
      },
    ],
  },
  privacy: {
    meta: {
      title: 'Privacy policy (Datenschutzerklärung) · OBIE',
      description:
        'How the OBIE website handles personal data: no cookies, no tracking, no third-party requests; what the inquiry form stores and for how long.',
    },
    heading: 'Privacy policy',
    legalTerm: 'Datenschutzerklärung',
    updated: 'Last updated: 28 September 2026',
    intro:
      'This policy explains which personal data Cloudwerks Technology GmbH (“we”) processes when you visit this website or send an inquiry, for what purpose, on which legal basis and for how long (Art. 13 of the General Data Protection Regulation, GDPR). Terms such as “personal data” and “processing” have the meaning given in Art. 4 GDPR.',
    sections: [
      {
        id: 'summary',
        heading: 'In short',
        blocks: [
          {
            kind: 'list',
            items: [
              'No cookies. This website sets no cookies and stores nothing else in your browser (no local storage, no session storage).',
              'No tracking and no analytics. We do not measure your visit, build profiles or use advertising services.',
              'No third-party requests. Fonts, images and scripts come from this website’s own server; your browser does not contact Google Fonts, content delivery networks or any other third party.',
              'Because there are no cookies and no tracking, this website shows no cookie banner: there is nothing to consent to.',
              'We only receive what you type into the inquiry form, and we use it only to answer you.',
            ],
          },
        ],
      },
      {
        id: 'controller',
        heading: 'Controller',
        legalTerm: 'Verantwortlicher',
        blocks: [
          {
            kind: 'facts',
            items: [
              { term: 'Controller', lines: ['Cloudwerks Technology GmbH', ...OPERATOR_ADDRESS] },
              { term: 'Represented by', lines: ['Markus Niewerth, managing director'] },
              { term: 'E-mail', lines: [OPERATOR_EMAIL], href: `mailto:${OPERATOR_EMAIL}` },
              { term: 'Phone', lines: [OPERATOR_PHONE], href: 'tel:+4917670526593' },
            ],
          },
          {
            kind: 'paragraph',
            text: 'We are not required by law to appoint a data protection officer and have not appointed one.',
          },
        ],
      },
      {
        id: 'hosting',
        heading: 'Hosting',
        blocks: [
          {
            kind: 'paragraph',
            text: 'This website, including its database, runs on servers of TODO(operator): name and address of the hosting provider. The provider processes data only on our behalf and under our instructions (processor, Art. 28 GDPR).',
          },
        ],
      },
      {
        id: 'server-logs',
        heading: 'Server log files',
        legalTerm: 'Server-Logfiles',
        blocks: [
          {
            kind: 'paragraph',
            text: 'When you open a page, the web server automatically records the data your browser sends with every request:',
          },
          {
            kind: 'list',
            items: [
              'the page or file requested, with date and time',
              'the HTTP status code and the amount of data transferred',
              'the referring page, if your browser sends it',
              'browser and operating system (user agent)',
              'the IP address of your device',
            ],
          },
          {
            kind: 'paragraph',
            text: `We need these logs to run the website securely and reliably, for example to detect and investigate attacks; this is our legitimate interest (Art. 6(1)(f) GDPR). The logs are kept for at most ${SERVER_LOG_RETENTION} and then deleted. Data needed as evidence of a specific incident is kept until the incident is resolved. The website application itself writes no access log; its own log records only technical events, such as the reference number of an inquiry, never your IP address or what you typed.`,
          },
        ],
      },
      {
        id: 'cookies',
        heading: 'Cookies, tracking and third-party content',
        legalTerm: 'Cookies und Tracking',
        blocks: [
          {
            kind: 'paragraph',
            text: 'This website sets no cookies and reads or stores no information on your device beyond what is technically required to deliver the page you asked for (§ 25 TDDDG). If you switch between the light and the dark theme, the choice lasts for your current visit only and is not stored. We use no analytics or tracking tools and no advertising services. That is why this website shows no cookie banner.',
          },
          {
            kind: 'paragraph',
            text: 'The fonts (Inter and Source Code Pro) are served from this website’s own server; there is no connection to Google Fonts or any other font service. We embed no content from other providers, such as videos, maps or social media buttons.',
          },
        ],
      },
      {
        id: 'inquiries',
        heading: 'Inquiry form',
        legalTerm: 'Kontaktformular',
        blocks: [
          {
            kind: 'paragraph',
            text: 'If you send an inquiry, for example to invite Markus Niewerth to give a talk, we process the following data:',
          },
          {
            kind: 'list',
            items: [
              'the kind of inquiry (talk, workshop, interview, collaboration or other)',
              'your name and e-mail address',
              'optionally: your organisation, and for events the date, location and expected audience',
              'your message',
              'the time the inquiry was received',
            ],
          },
          {
            kind: 'paragraph',
            text: 'Purpose and legal basis: we use this data only to answer your inquiry and, where it leads to one, to prepare an agreement, for example about a talk (Art. 6(1)(b) GDPR). For other inquiries the legal basis is our legitimate interest in answering the questions people send us (Art. 6(1)(f) GDPR). You are not obliged to provide the data, but without it we cannot answer you.',
          },
          {
            kind: 'paragraph',
            text: `Storage: the form is sent encrypted (HTTPS) to this website’s server, which stores the inquiry in its own PostgreSQL database. It is deleted automatically ${INQUIRY_RETENTION} after it was received.`,
          },
          {
            kind: 'paragraph',
            text: 'Forwarding by e-mail: after storing it, the server e-mails the inquiry to us and sends you a short confirmation that repeats nothing you typed. The e-mails are sent encrypted (TLS) through TODO(operator): name and address of the e-mail (SMTP) provider, which processes them only on our behalf (processor, Art. 28 GDPR). The copy in our mailbox is kept TODO(operator): how long answered inquiries stay in the mailbox.',
          },
          {
            kind: 'paragraph',
            text: 'Protection against abuse: to stop automated spam and floods of inquiries, the server limits the number of inquiries per IP address. For this, your IP address (for IPv6, its network part) is held in the server’s memory only, never written to disk, and removed once your allowance has refilled: with the default settings no later than 70 minutes after your last inquiry. With the inquiry itself we store your IP address only as a salted hash: a value from which the address cannot be read back, but which lets us recognise several inquiries from the same address. The hash is deleted together with the inquiry. The legal basis is our legitimate interest in protecting the form from abuse (Art. 6(1)(f) GDPR).',
          },
          {
            kind: 'paragraph',
            text: 'We do not pass your inquiry on to anyone else, and we do not use it for advertising or newsletters.',
          },
        ],
      },
      {
        id: 'external-links',
        heading: 'Links to other websites',
        legalTerm: 'Externe Links',
        blocks: [
          {
            kind: 'paragraph',
            text: 'This website links to other websites, mainly GitHub and LinkedIn. Only when you follow such a link does your browser contact that website; its own privacy policy then applies.',
          },
        ],
      },
      {
        id: 'third-countries',
        heading: 'Transfers outside the EU',
        legalTerm: 'Übermittlung in Drittländer',
        blocks: [
          {
            kind: 'paragraph',
            text: 'We do not transfer personal data collected on this website to countries outside the European Union or the European Economic Area. TODO(operator): confirm that the hosting and the e-mail provider process data only within the EU/EEA; otherwise name the transfer and its safeguard (Art. 44 ff. GDPR).',
          },
        ],
      },
      {
        id: 'rights',
        heading: 'Your rights',
        legalTerm: 'Ihre Rechte',
        blocks: [
          {
            kind: 'paragraph',
            text: 'You have the right to access the data we hold about you (Art. 15 GDPR), to have it corrected (Art. 16) or erased (Art. 17), to restrict its processing (Art. 18) and to receive it in a portable format (Art. 20).',
          },
          {
            kind: 'paragraph',
            text: 'Right to object: where we process data on the basis of our legitimate interest (Art. 6(1)(f) GDPR), you may object at any time on grounds relating to your particular situation (Art. 21 GDPR).',
          },
          {
            kind: 'paragraph',
            text: 'You also have the right to lodge a complaint with a data protection supervisory authority (Art. 77 GDPR). The authority responsible for us is the Landesbeauftragte für Datenschutz und Informationsfreiheit Nordrhein-Westfalen.',
          },
        ],
      },
      {
        id: 'privacy-contact',
        heading: 'Contact for privacy requests',
        blocks: [
          {
            kind: 'paragraph',
            text: 'To exercise your rights, or for any question about this policy, write to ',
            link: { label: OPERATOR_EMAIL, href: `mailto:${OPERATOR_EMAIL}` },
            after: '. You can ask us at any time to delete an inquiry you sent.',
          },
        ],
      },
      {
        id: 'changes',
        heading: 'Changes to this policy',
        legalTerm: 'Änderungen',
        blocks: [
          {
            kind: 'paragraph',
            text: 'We update this policy when the website or the law changes. The version published here applies.',
          },
        ],
      },
    ],
  },
};

/** Copy of the legal pages; provide another `LegalContent` for a new language. */
export const LEGAL_CONTENT = new InjectionToken<LegalContent>('LEGAL_CONTENT', {
  providedIn: 'root',
  factory: () => LEGAL_CONTENT_EN,
});
