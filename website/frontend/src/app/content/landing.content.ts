import { InjectionToken, inject } from '@angular/core';
import { TranslocoService } from '@jsverse/transloco';

import { PAGE_PATHS } from '../i18n/languages';
import { translated } from '../i18n/translated';
import { LandingContent } from './landing-content.model';

// All landing page copy (ADR 0012). Facts come from README.md,
// ARCHITECTURE.md and documentation/spec/obie-0.1.md; anything the code does
// not do yet is labelled "in progress" or "planned". Write for readers
// without a security background: short sentences, explain terms on first use.
// The German copy (landing.content.de.ts) says the same; change both together.

/** The public repository; every section's next step points here. */
export const REPOSITORY_URL =
  'https://github.com/MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange';

// Development happens on `develop`; `main` is only updated for releases.
const DOCS_URL = `${REPOSITORY_URL}/blob/develop`;

export const LINKS = {
  repository: REPOSITORY_URL,
  introduction: `${DOCS_URL}/documentation/introduction.md`,
  capabilities: `${DOCS_URL}/documentation/capabilities.md`,
  quickStart: `${DOCS_URL}/documentation/getting-started.md`,
  sandbox: `${DOCS_URL}/documentation/sandbox.md`,
  spec: `${DOCS_URL}/documentation/spec/obie-0.1.md`,
  whitepaper: `${DOCS_URL}/documentation/whitepaper.md#1-introduction-the-centralization-trap`,
  manifesto: `${DOCS_URL}/documentation/whitepaper.md#2-the-obie-manifesto-principles-and-philosophy`,
  licence: `${DOCS_URL}/LICENSE.md`,
  issues: `${REPOSITORY_URL}/issues`,
  goodFirstIssues: `${REPOSITORY_URL}/issues?q=is%3Aissue%20is%3Aopen%20label%3A%22good%20first%20issue%22`,
  securityPolicy: `${DOCS_URL}/SECURITY.md`,
  impressum: PAGE_PATHS.impressum.en,
  privacy: PAGE_PATHS.privacy.en,
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
    language: { label: 'DE', name: 'Deutsch' },
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
      'OBIE runs as a small program next to the tools you already use. Here is what happens in version 0.1 when one server sees an attack.',
    steps: [
      {
        title: 'Detect',
        text: 'A tool that already watches your logs spots an attack. The first one OBIE works with is Fail2Ban, a widely used program that notices repeated failed logins.',
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
    note: 'All six steps run in the code of version 0.1 today. What it cannot do yet is listed under Status.',
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
    // The demo's story (ADR 0028). Its numbers come from
    // sections/mesh-demo/scenario.ts and follow the node's decision rule;
    // keep the settings sentence in step with it (tests check the numbers).
    demo: {
      heading: 'See it happen: three servers, step by step.',
      intro:
        'A short story with three OBIE servers, two attackers and one troublemaker. Step through it at your own pace and watch each server decide for itself.',
      illustration:
        'Illustration: made-up servers and example addresses, not live data from the network.',
      settings:
        'Here, a server blocks an address when the reports it trusts reach a combined score of 1.2, its threshold, from at least two reporters, its quorum. The federation guide suggests this for three to five servers; out of the box the threshold is 1.8. A Fail2Ban report has a confidence of 0.8. These servers block in their firewalls; a new server only watches (observe mode) until its operator switches blocking on.',
      steps: [
        {
          title: 'Meet the neighbourhood',
          caption:
            'Three servers, each run by a different operator: a web shop (A), a university lab (B) and a homelab (C). They connect directly, with no central server. Each lists how much it trusts the others, from 0 to 1.',
          planned: 'Peers are added by hand today. Finding them automatically is planned.',
        },
        {
          title: 'The bot hits server A',
          caption:
            'A password-guessing bot works through server after server and starts with A. A’s own log watcher (such as Fail2Ban) blocks it at once: A needs nobody’s permission to protect itself. A colleague’s mistyped passwords get an office address blocked too.',
        },
        {
          title: 'A shares a signed report',
          caption:
            'A sends B and C a short report: the address, what it did, how often and a fingerprint of the evidence. The logs themselves stay on A. A’s digital signature proves to B and C that the report is A’s.',
        },
        {
          title: 'One voice is not enough',
          caption:
            'B and C record A’s reports but do not block. Each scores a report: its trust in the sender times how sure the sender is. One report stays below each server’s bar, and each wants two independent reporters.',
          note: 'One mistaken or compromised server must never get an address blocked everywhere. The office address shows why.',
        },
        {
          title: 'The bot moves on to server B',
          caption:
            'Next the bot tries B. B’s own detection catches it, and B blocks it at once. Its own report and A’s earlier one agree: two trusted voices.',
        },
        {
          title: 'C is protected before the attack arrives',
          caption:
            'B shares its signed report with A and C. Together with A’s, two independent, trusted reports now pass C’s bar, so C blocks the bot. When the bot knocks on C minutes later, it is turned away at the door.',
          planned:
            'Today the quorum counts servers, not organisations. Checking that reporters come from different networks is planned.',
        },
        {
          title: 'The scanner only hits server C',
          caption:
            'A web scanner probes C and nowhere else. C blocks it and reports it. A and B only watch: one reporter is not enough for them, and each weighs C by its own trust. Each server decides for itself.',
        },
        {
          title: 'Someone tries to abuse the mesh',
          caption:
            'An unknown participant floods the servers with reports to get the shop’s payment service blocked. Nobody trusts it, so its reports weigh 0, however many it sends. And the service is on A’s safety list: never blocked, whatever anyone reports.',
          planned:
            'Trust that grows or shrinks with a peer’s track record is planned. Today each operator sets the numbers.',
        },
        {
          title: 'Mistakes can be undone',
          caption:
            'A learns the office address is a colleague’s shared connection and withdraws its report with a signed revocation. A unblocks it; B and C drop it automatically. Blocks also end on their own: the scanner’s one-hour block has run out.',
          planned: 'A way for the owner of a blocked address to appeal is planned.',
        },
        {
          title: 'Recap',
          caption:
            'Shared intelligence, sovereign enforcement: servers warn each other early, and every server still decides for itself what to block.',
        },
      ],
      servers: {
        a: {
          name: 'Server A',
          short: 'A',
          operator: 'Web shop',
          safetyList: 'own networks, payment service',
        },
        b: {
          name: 'Server B',
          short: 'B',
          operator: 'University lab',
          safetyList: 'own networks, campus network',
        },
        c: {
          name: 'Server C',
          short: 'C',
          operator: 'Homelab',
          safetyList: 'own networks, home network',
        },
      },
      rogue: { name: 'Unknown participant', short: 'R' },
      subjects: {
        bot: { name: 'Password bot', short: 'bot' },
        office: { name: 'Office address', short: 'office' },
        scanner: { name: 'Web scanner', short: 'scanner' },
        payment: { name: 'Payment service', short: 'payment' },
      },
      states: {
        unknown: 'Unknown',
        watching: 'Watching, not blocked',
        blocked: 'Blocked',
        safe: 'Never blocked',
      },
      causes: {
        none: 'no reports',
        'below-bar': 'below the bar',
        agreement: 'enough trusted reporters agree',
        'own-detection': 'own detection',
        'safety-list': 'on the safety list',
      },
      labels: {
        trusts: 'Trusts',
        anyoneElse: 'anyone else {weight}',
        safetyList: 'Safety list',
        score: 'Score {score} of {threshold} needed',
        reporters: '{count} of {quorum} reporters',
        changed: 'Changed',
        turnedAway: 'turned away',
        copies: '×{n}',
        planned: 'Planned',
        note: 'Why',
      },
      controls: {
        label: 'Demo controls',
        restart: 'Restart',
        previous: 'Previous',
        next: 'Next',
        play: 'Play',
        pause: 'Pause',
        steps: 'Steps',
        stepOf: 'Step {n} of {total}',
        goTo: 'Step {n}: {title}',
        announcement: 'Step {n} of {total}: {title}. {caption}',
        transcript: 'All ten steps as text',
        servers: 'What each server decides',
      },
      report: {
        heading: 'What {server} shares',
        fields: {
          address: 'Address',
          reason: 'What it did',
          events: 'How often',
          fingerprint: 'Evidence',
          suggestion: 'Suggests',
          confidence: 'Confidence',
          signature: 'Signature',
        },
        reasons: { password_bruteforce: 'password guessing', web_scan: 'web scanning' },
        eventCount: '{n} failed logins',
        fingerprintNote: 'a fingerprint of the log lines; it cannot be turned back into them',
        suggestionValue: 'block for {duration}',
        signatureValue: 'Ed25519, checked by {receivers}',
        keptHeading: 'What stays on {server}',
        kept: ['the log lines themselves', 'user names and passwords', 'customer data'],
      },
      recap: {
        takeaways: [
          'Every server protects itself first.',
          'Sharing lets others act earlier.',
          'Nobody can order anyone else to block.',
        ],
        actions: [
          { label: 'View on GitHub', href: LINKS.repository },
          { label: 'Get started', href: '#get-started' },
        ],
      },
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
      'Version 0.1 works end to end in the code: a server turns Fail2Ban bans into signed reports, shares them with the peers you choose, decides for itself and, once you switch blocking on, blocks in its firewall. The first release is not published yet, there is no public mesh to join, and no independent security audit has been done. Here is what the code does today and what comes next.',
    groups: [
      {
        state: 'available',
        label: 'Available',
        summary: 'In the code today',
        items: [
          {
            title: 'Signed reports',
            text: 'Each report carries its server’s signature. The format has a public specification, with test data for other implementations.',
          },
          {
            title: 'Reports from Fail2Ban',
            text: 'One extra line in a Fail2Ban jail turns its bans into signed reports. The log lines stay on your server.',
          },
          {
            title: 'Sharing with peers you choose',
            text: 'Servers connect directly to the peers you list. Invalid reports are dropped, and each sender is limited in how many it can send.',
          },
          {
            title: 'Decisions on each server',
            text: 'Trust weights per peer, a minimum number of agreeing peers and a score to reach. The node explains why it blocks an address, or why not.',
          },
          {
            title: 'Safety list and overrides',
            text: 'Your own addresses, internal networks and your peers are never blocked. Add the networks you depend on, and allow or block any other address yourself.',
          },
          {
            title: 'Observe first, then block',
            text: 'A new server only shows what it would block. In enforce mode, it blocks in its own nftables table, and every block expires.',
          },
          {
            title: 'Setup, self-check and web console',
            text: 'A setup assistant, a self-check that warns before you could lock yourself out, an optional web console, metrics and an audit log.',
          },
          {
            title: 'A sandbox to try it',
            text: 'Four nodes in Docker on your own computer, with a step-by-step walkthrough. Nothing is ever blocked.',
          },
        ],
      },
      {
        state: 'in-progress',
        label: 'In progress',
        summary: 'Being prepared and designed now',
        items: [
          {
            title: 'The first release',
            text: 'Release packages, an installer, a hardened service and a container image are ready and tested on every change. The release itself is not published yet.',
          },
          {
            title: 'Reliable delivery',
            text: 'Simulations of 1,000 to 10,000 servers found that in a large mesh built around a few hubs, about one report in six never arrives, and a server that was offline misses what was sent meanwhile. Fixes are being designed.',
          },
          {
            title: 'Resisting floods and forgeries',
            text: 'In the same simulations, a flood from untrusted senders pushed trusted reports out of a full store, and forged messages kept a withdrawal from most servers. Defences are being designed.',
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
      label: 'What version 0.1 can and cannot do yet, and what it needs',
      href: LINKS.capabilities,
    },
    nextStep: { label: 'Follow the progress on GitHub', href: LINKS.repository },
  },
  getStarted: {
    id: 'get-started',
    label: 'Get started',
    heading: 'Try it in three steps.',
    intro:
      'OBIE is built for engineers who run their own Linux servers. Watch it work on your own computer first, then run it on a server.',
    steps: [
      {
        title: 'Try it in a sandbox',
        text: 'In a copy of the source code, start four OBIE nodes in Docker on your own computer. Report an attack, watch the others decide and ask them why. Nothing is ever blocked, and you need no root access.',
        code: 'cd packaging/sandbox && ./sandbox up',
      },
      {
        title: 'Install and observe',
        text: 'Install OBIE on a Linux server with systemd and answer the setup assistant’s five questions. The node starts in observe mode: it shows what it would block, and blocks nothing.',
        code: 'sudo obied setup',
      },
      {
        title: 'Check, connect, then block',
        text: 'The self-check says what is right and what to fix next. Connect Fail2Ban and a peer you trust. Switch blocking on only once you are sure you cannot lock yourself out.',
        code: 'sudo obied self-check',
      },
    ],
    note: 'The release packages with the installer come with the first release, which is not published yet. Until then, OBIE is built from source with Go 1.26 or newer; the sandbox does that for you.',
    quickStart: { label: 'The sandbox walkthrough, step by step', href: LINKS.sandbox },
    project: {
      linksLabel: 'OBIE on GitHub',
      links: [
        { label: 'Repository', href: LINKS.repository },
        { label: 'Getting started', href: LINKS.quickStart },
        { label: 'Protocol specification', href: LINKS.spec },
        { label: 'Good first issues', href: LINKS.goodFirstIssues },
      ],
      statsCaption: 'The project on GitHub',
      stars: 'Stars',
      latestRelease: 'Latest release',
      noRelease: 'None yet',
      lastActivity: 'Last commit',
    },
    nextStep: { label: 'Open the step-by-step guide on GitHub', href: LINKS.quickStart },
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
          'Not on its own. In version 0.1, your server only acts on reports from sources you chose to trust, by default only when at least two of them report the same address (your own server counts as one) with enough combined confidence, and never against your safety list. Reports about private and internal network addresses are rejected outright. Peers you trust could still agree on a wrong report, which is why you choose them carefully and can start in observe-only mode. Trust that is earned automatically is planned.',
      },
      {
        question: 'What data leaves my server?',
        answer:
          'Only signed reports, in the format the specification defines: the attacking address, the attacked service, how many events were seen, a reason code, whether a honeypot saw it, the suggested action, a confidence value and how long the action should last. Optional are a fingerprint of the log lines, which proves what you saw without revealing it, codes for the attack technique (MITRE ATT&CK IDs) and the number of your network (its ASN). A server can also withdraw its own report with a signed revocation that carries a reason code. The format has no room for logs, user names, passwords or free text. Every node connected to your mesh receives your reports, whether you trust it or not, and sees your server’s address and its public OBIE ID.',
      },
      {
        question: 'Do I need Fail2Ban?',
        answer:
          'Fail2Ban is the first detector OBIE works with: one extra line in a jail turns its bans into signed reports. A server does not need its own detector to act on reports from peers it trusts, and any tool that can run a command can report an address. Ready-made support for honeypots (decoy servers that attract attackers) is planned.',
      },
      {
        question: 'Is it production-ready?',
        answer:
          'Not yet. Version 0.1 works end to end: it reports, shares, decides and, once you switch blocking on, blocks. But the first release is not published yet, no independent security audit has been done, and simulations have found weaknesses that are still to be fixed (see Status). If you try it on a server, start in observe mode, which blocks nothing, and read what OBIE cannot do yet.',
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
      { label: 'What OBIE can and cannot do', href: LINKS.capabilities },
      { label: 'Specification', href: LINKS.spec },
      { label: 'Security policy', href: LINKS.securityPolicy },
      { label: 'Invite Markus to speak', href: `#${CONTACT_ID}` },
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

/** The landing page copy in the page's language (Transloco translation `landing`). */
export const LANDING_CONTENT = new InjectionToken<LandingContent>('LANDING_CONTENT', {
  providedIn: 'root',
  factory: () => translated<LandingContent>(inject(TranslocoService), 'landing'),
});
