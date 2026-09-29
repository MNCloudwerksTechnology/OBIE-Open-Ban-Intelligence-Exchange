import { InjectionToken } from '@angular/core';

import { LandingContent } from './landing-content.model';

// All landing page copy (ADR 0012). Facts come from README.md,
// ARCHITECTURE.md and documentation/spec/obie-0.1.md; anything the code does
// not do yet is labelled "in progress" or "planned". Write for readers
// without a security background: short sentences, explain terms on first use.

/** The public repository; every section's next step points here. */
export const REPOSITORY_URL =
  'https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange';

// Development happens on `develop`; `main` is only updated for releases.
const DOCS_URL = `${REPOSITORY_URL}/blob/develop`;

export const LINKS = {
  repository: REPOSITORY_URL,
  introduction: `${DOCS_URL}/documentation/introduction.md`,
  quickStart: `${DOCS_URL}/documentation/operations/quickstart.md`,
  spec: `${DOCS_URL}/documentation/spec/obie-0.1.md`,
  whitepaper: `${DOCS_URL}/documentation/whitepaper.md#1-introduction-the-centralization-trap`,
  manifesto: `${DOCS_URL}/documentation/whitepaper.md#2-the-obie-manifesto-principles-and-philosophy`,
  architecture: `${DOCS_URL}/ARCHITECTURE.md#deviations-from-the-whitepaper`,
  exampleConfig: `${DOCS_URL}/documentation/examples/obie.yaml`,
  licence: `${DOCS_URL}/LICENSE.md`,
  issues: `${REPOSITORY_URL}/issues`,
  goodFirstIssues: `${REPOSITORY_URL}/issues?q=is%3Aissue%20is%3Aopen%20label%3A%22good%20first%20issue%22`,
  securityPolicy: `${DOCS_URL}/SECURITY.md`,
  impressum: '/impressum',
  privacy: '/privacy',
  cloudwerks: 'https://cloudwerks.de',
} as const;

/** Anchor of the inquiry form; `/#contact` deep-links to it. */
export const CONTACT_ID = 'contact';

/** Neutral avatar shown until the operator supplies a headshot. */
export const FOUNDER_AVATAR_PLACEHOLDER = '/founder/avatar-placeholder.svg';

/** English landing page copy. */
export const LANDING_CONTENT_EN: LandingContent = {
  meta: {
    title: 'OBIE: open-source threat intelligence sharing for servers',
    description:
      'OBIE is an open, leaderless protocol for sharing signed attacker signals between servers, where every node keeps the final say over what it blocks.',
    locale: 'en-GB',
  },
  a11y: {
    skipLink: 'Skip to content',
    primaryNav: 'Sections of this page',
    footerNav: 'Project links',
    homeLink: 'OBIE, back to the top of the page',
    themeToDark: 'Switch to dark theme',
    themeToLight: 'Switch to light theme',
  },
  header: {
    github: { label: 'View on GitHub', href: LINKS.repository },
    invite: { label: 'Invite Markus to speak', href: `#${CONTACT_ID}` },
  },
  hero: {
    eyebrow: 'OBIE · Open Ban Intelligence Exchange',
    heading: 'A neighbourhood watch for servers.',
    lead: 'OBIE is a leaderless mesh where defenders exchange signed attacker signals, so that collective defence stops requiring a central authority anyone has to trust. Servers warn each other about attackers. Each server still decides for itself what to block.',
    primary: { label: 'View on GitHub', href: LINKS.repository },
    secondary: { label: 'How it works', href: '#how-it-works' },
    challenge: { label: 'Read the spec and try to break it', href: LINKS.spec },
    noTokens: 'No tokens. No coin. Incentives come from mutual defence, not speculation.',
    report: {
      caption:
        'What a server shares under the version 0.1 specification: a short, signed report. Never logs, never user data.',
      title: 'signed report',
      rows: [
        { key: 'address', value: '203.0.113.7' },
        { key: 'service', value: 'ssh' },
        { key: 'seen', value: '47 failed logins' },
        { key: 'suggests', value: 'block for 7 days' },
        { key: 'confidence', value: '0.92' },
      ],
      signature: 'signed · ed25519 · verified',
    },
  },
  problem: {
    id: 'problem',
    label: 'The problem',
    heading: 'Every server fights the same attackers alone.',
    hook: 'Defensive intelligence is concentrated in a handful of vendors. Their outage is your outage.',
    paragraphs: [
      'Any server on the internet is probed all day by automated tools that guess passwords and look for weak spots. The same addresses attack thousands of servers. Yet each server has to learn about an attacker the hard way: by being attacked.',
      'The usual shortcut is a threat feed: a list of known bad addresses that one provider collects and hands out. That helps, but it moves the problem instead of solving it.',
    ],
    points: [
      {
        title: 'Paywalled',
        text: 'Good feeds often cost money. Small operators, who need help most, go without.',
      },
      {
        title: 'Opaque',
        text: 'You cannot see why an address is on the list. You have to trust the provider.',
      },
      {
        title: 'A single point of failure',
        text: 'If the provider has an outage, makes a mistake or is compromised, everyone who relies on it is affected at once.',
      },
    ],
    answer:
      'OBIE takes a different route: servers share what they have seen directly with each other, every report can be checked, and nobody in the middle decides for you.',
    nextStep: { label: 'Read the full reasoning in the whitepaper', href: LINKS.whitepaper },
  },
  howItWorks: {
    id: 'how-it-works',
    label: 'How it works',
    heading: 'Six steps from one attack to shared protection.',
    intro:
      'OBIE runs as a small program next to the tools you already use. Here is what happens when one server sees an attack, as designed for version 0.1.',
    steps: [
      {
        title: 'Detect',
        text: 'A tool that already watches your logs spots an attack. The first one OBIE is being built for is Fail2Ban, a widely used program that notices repeated failed logins.',
      },
      {
        title: 'Sign a report',
        text: 'Your server writes a short report about the attacking address and signs it with its own key. The signature is a digital seal: anyone can check who wrote the report and that nobody changed it. Your logs and your users’ data stay at home.',
      },
      {
        title: 'Share with peers you trust',
        text: 'The report goes to other OBIE servers, called peers. In version 0.1 you choose these peers yourself, and you decide how much you trust each one.',
      },
      {
        title: 'Each server decides for itself',
        text: 'No report is an order. Every server weighs what it receives by how much it trusts the sender. By default it only acts when at least two trusted sources report the same address (your own server counts as one) and their combined confidence is high enough.',
      },
      {
        title: 'The safety list always wins',
        text: 'Addresses you must never block, such as your office network or your own gateways, go on a safety list (an allowlist). No report can override it.',
      },
      {
        title: 'Block, then expire',
        text: 'When the rules are met, the server blocks the address in its firewall for a limited time. The block ends on its own, so a mistake does not last forever.',
      },
    ],
    note: 'This is the version 0.1 design. Which steps already run in the code is listed under Status.',
    diagram: {
      title: 'How a block comes about',
      description:
        'An attacker hits peer A and peer B. Each sends a signed report to your server. Your server checks the reports against the peers it trusts and its safety list, and then blocks the attacker for a limited time.',
      attacker: 'Attacker',
      peerA: 'Peer A',
      peerB: 'Peer B',
      you: 'Your server',
      report: 'signed report',
      decision: '2 trusted reports',
      safetyList: 'safety list checked',
      block: 'block · expires',
    },
    nextStep: {
      label: 'Read the plain-language introduction: what OBIE is, in five minutes',
      href: LINKS.introduction,
    },
  },
  principles: {
    id: 'principles',
    label: 'Principles',
    heading: 'Rules that keep OBIE a shield, not a weapon.',
    hook: 'Ten principles, and the first is: evidence above authority.',
    cards: [
      {
        title: 'Evidence above authority',
        text: 'Trust comes from data you can check, not from a badge. Every report is signed, so you always know who sent it.',
      },
      {
        title: 'Local sovereignty',
        text: 'Your server makes its own decisions. Reports from others are advice, never orders.',
      },
      {
        title: 'No central kill switch',
        text: 'There is no central server that can switch OBIE off or tell everyone what to block.',
      },
      {
        title: 'Privacy by default',
        text: 'Servers share attacker addresses. They never share your users’ identities or your raw logs.',
      },
      {
        title: 'No tokens, no speculation',
        text: 'There is no coin and nothing to trade. You take part because shared defence protects you too.',
      },
      {
        title: 'Practical to deploy',
        text: 'If it can’t be deployed by a competent engineer in a weekend, it’s research, not production.',
      },
    ],
    nextStep: { label: 'Read all ten principles', href: LINKS.manifesto },
  },
  status: {
    id: 'status',
    label: 'Status',
    heading: 'Where OBIE stands today.',
    intro:
      'Version 0.1 is being built in the open. There is no running public mesh yet, and OBIE is not ready to protect production servers. Here is what the code does today and what comes next.',
    groups: [
      {
        state: 'available',
        label: 'Available',
        summary: 'In the code today',
        items: [
          {
            title: 'Signed report format',
            text: 'A public specification of what a report contains and how it is signed, with test data for anyone who wants to build their own implementation.',
          },
          {
            title: 'Node identity',
            text: 'On its first start, each server creates its own key. Its public ID is derived from it.',
          },
          {
            title: 'Connecting to peers you list',
            text: 'The node connects to the peers in its configuration file and reconnects when a connection drops.',
          },
          {
            title: 'Local report store',
            text: 'Reports are stored on the node, duplicates are dropped and expired reports are removed.',
          },
          {
            title: 'Operator tools',
            text: 'A command-line tool shows the node’s status, identity and peers. Health checks and metrics plug into existing monitoring.',
          },
        ],
      },
      {
        state: 'in-progress',
        label: 'In progress',
        summary: 'Being built for version 0.1',
        items: [
          {
            title: 'Detection with Fail2Ban',
            text: 'Turning Fail2Ban detections into signed reports.',
          },
          {
            title: 'Sharing reports',
            text: 'Sending reports to peers and receiving theirs.',
          },
          {
            title: 'Local decisions',
            text: 'Trust weights per peer, the two-source minimum, the safety list and observe-only mode.',
          },
          {
            title: 'Blocking that expires',
            text: 'Blocking addresses with nftables, the Linux firewall, for a limited time.',
          },
        ],
      },
      {
        state: 'planned',
        label: 'Planned',
        summary: 'Later releases',
        items: [
          {
            title: 'Automatic peer discovery',
            text: 'Finding other OBIE servers without listing each one by hand.',
          },
          {
            title: 'Earned reputation',
            text: 'Trust in a peer that grows or shrinks with how accurate its reports turn out to be.',
          },
          {
            title: 'Diversity checks',
            text: 'Acting only when reports come from several independent networks and organisations.',
          },
          {
            title: 'Appeals',
            text: 'A way for the owner of a blocked address to ask for a review.',
          },
          {
            title: 'eBPF blocking',
            text: 'Very fast filtering inside the Linux kernel for heavy attacks.',
          },
        ],
      },
    ],
    details: {
      label: 'How version 0.1 differs from the whitepaper',
      href: LINKS.architecture,
    },
    nextStep: { label: 'Follow the progress on GitHub', href: LINKS.repository },
  },
  getStarted: {
    id: 'get-started',
    label: 'Get started',
    heading: 'Try it in three steps.',
    intro:
      'OBIE is built for engineers who run their own Linux servers. You need Go 1.26 or newer to build it.',
    steps: [
      {
        title: 'Install',
        text: 'Build the node from source. One command produces two programs in ./bin: obied, the node, and obiectl, the tool to control it.',
        code: 'make build',
      },
      {
        title: 'Observe only',
        text: 'Start in observe-only mode, the default. Once decisions land, the node will record what it would block but block nothing, so you can check its judgement first.',
        code: './bin/obied --config /etc/obie/obie.yaml',
      },
      {
        title: 'Connect peers',
        text: 'List the peers you trust, and how much, in the configuration file. Once blocking lands, switch to enforce mode when you are confident.',
        code: './bin/obiectl --socket /run/obie/obie.sock peers',
      },
    ],
    note: 'Version 0.1 is still in development. Until decisions and blocking land (see Status), a node connects to its peers and stores reports, but decides and blocks nothing.',
    quickStart: { label: 'Example configuration with every option', href: LINKS.exampleConfig },
    project: {
      linksLabel: 'OBIE on GitHub',
      links: [
        { label: 'Repository', href: LINKS.repository },
        { label: 'Quick start', href: LINKS.quickStart },
        { label: 'Protocol specification', href: LINKS.spec },
        { label: 'Good first issues', href: LINKS.goodFirstIssues },
      ],
      statsCaption: 'The project on GitHub',
      stars: 'Stars',
      latestRelease: 'Latest release',
      noRelease: 'None yet',
      lastActivity: 'Last commit',
    },
    nextStep: { label: 'Open the quick start on GitHub', href: LINKS.quickStart },
  },
  // Founder facts come from the operator (work package #1675). Do not add
  // claims, quotes, testimonials or speaking history. website/README.md lists
  // the fields the operator still has to fill in.
  founder: {
    id: 'founder',
    label: 'Founder',
    heading: 'Who started OBIE.',
    name: 'Markus Niewerth',
    role: 'Founder of OBIE · Software architect · Managing director, Cloudwerks Technology GmbH',
    bio: 'Markus Niewerth has been building software systems that reduce complexity for 15 years. As founder of Cloudwerks Technology GmbH he designs and builds its products himself, among them QuickSelect, Krisis and OBIE, and works in parallel as a software architect on automotive platforms (software-defined vehicle, Android Automotive OS). He started OBIE after evaluating crowd-sourced security SDKs for a project and running into what he calls the centralization trap.',
    // TODO(operator): headshot. Put the photo into public/founder/, point
    // `src` at it and describe it in `alt`; keep `alt` empty for the placeholder.
    photo: { src: FOUNDER_AVATAR_PLACEHOLDER, alt: '' },
    topicsHeading: 'Proposed talk topics',
    topicsNote: 'Proposals drawn from OBIE’s principles. Suggest your own topic in the form below.',
    // TODO(operator): confirm talk topics (proposals derived from OBIE's
    // themes; none are on record yet).
    topics: [
      'The centralization trap: collective defence without a central authority',
      'Evidence above authority: designing a threat-sharing protocol you don’t have to trust',
      'Local sovereignty in practice: trust-weighted decisions and allow-lists that always win',
      'Boringly robust: security software a competent engineer can deploy in a weekend',
    ],
    linksLabel: 'Profiles',
    links: [
      { label: 'LinkedIn', href: 'https://www.linkedin.com/in/niewerth/' },
      { label: 'GitHub', href: 'https://github.com/MNCloudwerksTechnology' },
    ],
    invite: { label: 'Invite Markus to speak', href: `#${CONTACT_ID}` },
    nextStep: { label: 'Ask a question on GitHub', href: LINKS.issues },
  },
  contact: {
    id: CONTACT_ID,
    label: 'Contact',
    heading: 'Invite Markus to speak, or get in touch.',
    intro:
      'For talks, workshops, interviews, research collaborations and other questions about OBIE. Your message goes directly to Markus Niewerth.',
    form: {
      typeLegend: 'What is your inquiry about?',
      types: [
        { value: 'talk', label: 'Talk' },
        { value: 'workshop', label: 'Workshop' },
        { value: 'interview', label: 'Interview or press' },
        { value: 'collaboration', label: 'Collaboration or research' },
        { value: 'other', label: 'Other' },
      ],
      eventLegend: 'About the event',
      fields: {
        name: { label: 'Your name' },
        email: { label: 'E-mail address', hint: 'Only used to answer you.' },
        organisation: { label: 'Organisation' },
        eventDate: { label: 'Date' },
        eventLocation: { label: 'Location', hint: 'A city, a venue or “online”.' },
        audienceSize: { label: 'Expected audience', hint: 'Number of people.' },
        message: { label: 'Message', hint: 'At least 20 characters.' },
      },
      optional: '(optional)',
      consent: {
        before: 'I have read the ',
        link: { label: 'privacy notice', href: LINKS.privacy },
        after: ' and understand that my inquiry is stored to answer me.',
      },
      honeypot: 'Leave this field empty',
      submit: 'Send inquiry',
      sending: 'Sending…',
      invalid: 'Please check the marked fields.',
      success: {
        heading: 'Inquiry sent.',
        text: 'Thanks, Markus will get back to you within a few days.',
      },
      error: {
        heading: 'Your inquiry was not sent.',
        text: 'Something went wrong, on our side or with the connection. Your entries are still here, so please try again.',
        expired:
          'The form was open for a long time and had to be refreshed. Your entries are still here, so please send it again.',
        rateLimited: 'Too many inquiries came from your network. Please try again later.',
        retry: 'Try again',
      },
      // Worded exactly like the back end (InquiryRequest.java), so a visitor
      // sees the same message whichever side catches the mistake.
      messages: {
        typeRequired: 'Please choose what your inquiry is about.',
        nameRequired: 'Please enter your name.',
        emailRequired: 'Please enter your e-mail address.',
        emailInvalid: 'Please enter a valid e-mail address.',
        messageRequired: 'Please enter a message.',
        messageLength: 'Please write between 20 and 5000 characters.',
        maxLength: 'Please use at most {max} characters.',
        singleLine: 'Please use a single line.',
        controlCharacters: 'Please remove special control characters.',
        dateInFuture: 'Please choose a date in the future.',
        dateFormat: 'Please enter a date as YYYY-MM-DD.',
        positiveNumber: 'Please enter a positive number.',
        wholeNumber: 'Please enter a whole number.',
        maxAudience: 'Please enter at most 1,000,000.',
        consentRequired: 'Please accept the privacy notice.',
      },
    },
    nextStep: { label: 'Prefer to ask in public? Open an issue on GitHub', href: LINKS.issues },
  },
  faq: {
    id: 'faq',
    label: 'FAQ',
    heading: 'Honest answers.',
    items: [
      {
        question: 'Is it free?',
        answer:
          'Yes. The code and the specification are open source under the MIT licence. There are no tokens and no coin.',
      },
      {
        question: 'Can a malicious peer get an address blocked on my server?',
        answer:
          'Not on its own. As designed for version 0.1, your server only acts on reports from sources you chose to trust, by default only when at least two of them report the same address (your own server counts as one) with enough combined confidence, and never against your safety list. Reports about private and internal network addresses are rejected outright. Peers you trust could still agree on a wrong report, which is why you choose them carefully and can start in observe-only mode. Trust that is earned automatically is planned.',
      },
      {
        question: 'What data leaves my server?',
        answer:
          'Once sharing is built (in progress for version 0.1), only signed reports in the format the specification defines: the attacking address, the attacked service, how many events were seen, a reason code, whether a honeypot saw it, the suggested action, a confidence value and how long the action should last. Optional are a fingerprint of the log lines, which proves what you saw without revealing it, codes for the attack technique (MITRE ATT&CK IDs) and the number of your network (its ASN). A server can also withdraw its own report with a signed revocation that carries a reason code. The format has no room for logs, user names, passwords or free text. Like any network connection, your peers see your server’s address and its public OBIE ID.',
      },
      {
        question: 'Do I need Fail2Ban?',
        answer:
          'Fail2Ban is the first detector OBIE is being built to work with. A server does not need its own detector to act on reports from peers it trusts. Support for other sources, such as honeypots (decoy servers that attract attackers), is planned.',
      },
      {
        question: 'Is it production-ready?',
        answer:
          'No. Version 0.1 is in development in the open. Today a node runs, has its own identity, connects to the peers you list and stores reports. Detection, decisions and blocking are still being built. Please do not rely on it to protect production servers yet.',
      },
      {
        question: 'Is there a central server that can switch it off?',
        answer:
          'No. Servers connect directly to each other. There is no central server, no account and no kill switch.',
      },
      {
        question: 'Who is behind it?',
        answer:
          'OBIE is developed in the open on GitHub, with a public specification and MIT-licensed code. The company that initiated it is named in the footer, and the founder section above introduces the person who started it. Anyone can read the code, report problems and contribute.',
      },
    ],
    nextStep: { label: 'Ask your own question on GitHub', href: LINKS.issues },
  },
  footer: {
    tagline: 'OBIE: Shared Intelligence, Sovereign Enforcement.',
    github: { label: 'View on GitHub', href: LINKS.repository },
    links: [
      { label: 'What is OBIE?', href: LINKS.introduction },
      { label: 'Specification', href: LINKS.spec },
      { label: 'Security policy', href: LINKS.securityPolicy },
      { label: 'Impressum', href: LINKS.impressum },
      { label: 'Privacy', href: LINKS.privacy },
      { label: 'MIT licence', href: LINKS.licence },
    ],
    attribution: {
      text: 'An open protocol initiated by Cloudwerks Technology GmbH.',
      href: LINKS.cloudwerks,
    },
    licence: 'MIT licence · © 2026 Cloudwerks Technology GmbH',
  },
};

/** Copy of the landing page; provide another `LandingContent` for a new language. */
export const LANDING_CONTENT = new InjectionToken<LandingContent>('LANDING_CONTENT', {
  providedIn: 'root',
  factory: () => LANDING_CONTENT_EN,
});
