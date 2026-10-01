import { PAGE_PATHS, sectionHref } from '../i18n/languages';
import { LandingContent } from './landing-content.model';
import { CONTACT_ID, FOUNDER_AVATAR_PLACEHOLDER, LINKS } from './landing.content';

// German landing page copy: LANDING_CONTENT_EN (landing.content.ts) translated.
// Keep both files in step: same structure, same order, same facts.

/** German landing page copy. */
export const LANDING_CONTENT_DE: LandingContent = {
  meta: {
    title: 'OBIE: Open-Source-Austausch von Bedrohungsinformationen für Server',
    description:
      'OBIE ist ein offenes Protokoll ohne zentrale Leitung, über das Server signierte Hinweise auf Angreifer austauschen. Jeder Knoten behält das letzte Wort darüber, was er sperrt.',
    locale: 'de-DE',
  },
  a11y: {
    skipLink: 'Zum Inhalt springen',
    primaryNav: 'Abschnitte dieser Seite',
    footerNav: 'Projektlinks',
    homeLink: 'OBIE, zurück zum Seitenanfang',
    themeToDark: 'Zum dunklen Design wechseln',
    themeToLight: 'Zum hellen Design wechseln',
  },
  header: {
    // Shorter than elsewhere, so the header keeps to one row on phones and at 1024 px.
    github: { label: 'Zu GitHub', href: LINKS.repository },
    language: { label: 'EN', name: 'English' },
  },
  hero: {
    eyebrow: 'OBIE · Open Ban Intelligence Exchange',
    heading: 'Eine Nachbarschaftswache für Server.',
    lead: 'OBIE ist ein Mesh ohne zentrale Leitung, in dem Verteidiger signierte Hinweise auf Angreifer austauschen. So braucht gemeinsame Abwehr keine zentrale Instanz mehr, der alle vertrauen müssen. Server warnen einander vor Angreifern. Jeder Server entscheidet weiterhin selbst, was er sperrt.',
    primary: { label: 'Auf GitHub ansehen', href: LINKS.repository },
    secondary: { label: 'So funktioniert es', href: sectionHref('de', 'how-it-works') },
    challenge: { label: 'Die Spezifikation lesen und versuchen, sie zu knacken', href: LINKS.spec },
    noTokens:
      'Keine Tokens. Keine Kryptowährung. Der Anreiz ist gegenseitige Abwehr, nicht Spekulation.',
    report: {
      caption:
        'Was ein Server nach der Spezifikation der Version 0.1 teilt: eine kurze, signierte Meldung. Niemals Logs, niemals Nutzerdaten.',
      title: 'signierte Meldung',
      rows: [
        { key: 'address', value: '203.0.113.7' },
        { key: 'service', value: 'ssh' },
        { key: 'seen', value: '47 fehlgeschlagene Anmeldungen' },
        { key: 'suggests', value: 'für 7 Tage sperren' },
        { key: 'confidence', value: '0.92' },
      ],
      signature: 'signiert · ed25519 · geprüft',
    },
  },
  problem: {
    id: 'problem',
    label: 'Problem',
    heading: 'Jeder Server wehrt dieselben Angreifer allein ab.',
    hook: 'Das Wissen zur Abwehr liegt bei einer Handvoll Anbieter. Deren Ausfall ist Ihr Ausfall.',
    paragraphs: [
      'Jeder Server im Internet wird den ganzen Tag von automatisierten Programmen abgetastet, die Passwörter erraten und nach Schwachstellen suchen. Dieselben Adressen greifen Tausende Server an. Trotzdem muss jeder Server einen Angreifer auf die harte Tour kennenlernen: indem er angegriffen wird.',
      'Die übliche Abkürzung ist ein sogenannter Threat-Feed: eine Liste bekannter schädlicher Adressen, die ein Anbieter sammelt und verteilt. Das hilft, verschiebt das Problem aber nur, statt es zu lösen.',
    ],
    points: [
      {
        title: 'Kostenpflichtig',
        text: 'Gute Feeds kosten oft Geld. Kleine Betreiber, die Hilfe am dringendsten brauchen, gehen leer aus.',
      },
      {
        title: 'Undurchsichtig',
        text: 'Sie sehen nicht, warum eine Adresse auf der Liste steht. Sie müssen dem Anbieter vertrauen.',
      },
      {
        title: 'Ein einzelner Ausfallpunkt',
        text: 'Hat der Anbieter eine Störung, macht er einen Fehler oder wird er kompromittiert, sind alle, die sich auf ihn verlassen, gleichzeitig betroffen.',
      },
    ],
    answer:
      'OBIE geht einen anderen Weg: Server teilen ihre Beobachtungen direkt miteinander, jede Meldung lässt sich prüfen, und niemand in der Mitte entscheidet für Sie.',
    nextStep: { label: 'Die ganze Begründung im Whitepaper lesen', href: LINKS.whitepaper },
  },
  howItWorks: {
    id: 'how-it-works',
    // Section labels are also the header links: short, so the links keep the same
    // rows on phones in the fallback font and in Inter (no layout shift).
    label: 'Funktionsweise',
    heading: 'Sechs Schritte von einem Angriff zum gemeinsamen Schutz.',
    intro:
      'OBIE läuft als kleines Programm neben den Werkzeugen, die Sie schon nutzen. Hier sehen Sie, was in Version 0.1 passiert, wenn ein Server einen Angriff bemerkt.',
    steps: [
      {
        title: 'Erkennen',
        text: 'Ein Werkzeug, das Ihre Logdateien bereits überwacht, bemerkt einen Angriff. Das erste, mit dem OBIE zusammenarbeitet, ist Fail2Ban, ein weit verbreitetes Programm, das wiederholte fehlgeschlagene Anmeldungen erkennt.',
      },
      {
        title: 'Meldung signieren',
        text: 'Ihr Server schreibt eine kurze Meldung über die angreifende Adresse und signiert sie mit seinem eigenen Schlüssel. Die Signatur ist ein digitales Siegel: Jeder kann prüfen, wer die Meldung geschrieben hat und dass niemand sie verändert hat. Ihre Logs und die Daten Ihrer Nutzer bleiben bei Ihnen.',
      },
      {
        title: 'Mit Peers teilen, denen Sie vertrauen',
        text: 'Die Meldung geht an andere OBIE-Server, sogenannte Peers. In Version 0.1 wählen Sie diese Peers selbst aus und legen fest, wie sehr Sie jedem von ihnen vertrauen.',
      },
      {
        title: 'Jeder Server entscheidet selbst',
        text: 'Keine Meldung ist ein Befehl. Jeder Server gewichtet, was er erhält, danach, wie sehr er dem Absender vertraut. Standardmäßig handelt er erst, wenn mindestens zwei vertrauenswürdige Quellen dieselbe Adresse melden (Ihr eigener Server zählt als eine) und ihre gemeinsame Konfidenz (wie sicher sie sich sind) hoch genug ist.',
      },
      {
        title: 'Die Schutzliste hat immer Vorrang',
        text: 'Adressen, die Sie niemals sperren dürfen, etwa Ihr Büronetz oder Ihre eigenen Gateways, kommen auf eine Schutzliste (eine Allowlist). Keine Meldung kann sie aushebeln.',
      },
      {
        title: 'Sperren, dann ablaufen lassen',
        text: 'Sind die Regeln erfüllt, sperrt der Server die Adresse in seiner Firewall für eine begrenzte Zeit. Die Sperre endet von selbst, damit ein Fehler nicht ewig bestehen bleibt.',
      },
    ],
    note: 'Alle sechs Schritte laufen heute im Code von Version 0.1. Was sie noch nicht kann, steht unter „Status“.',
    diagram: {
      title: 'Wie eine Sperre zustande kommt',
      description:
        'Ein Angreifer trifft Peer A und Peer B. Beide schicken Ihrem Server eine signierte Meldung. Ihr Server prüft die Meldungen gegen die Peers, denen er vertraut, und gegen seine Schutzliste und sperrt den Angreifer dann für eine begrenzte Zeit.',
      attacker: 'Angreifer',
      peerA: 'Peer A',
      peerB: 'Peer B',
      you: 'Ihr Server',
      report: 'signierte Meldung',
      decision: '2 vertraute Meldungen',
      safetyList: 'Schutzliste geprüft',
      block: 'Sperre · läuft ab',
    },
    demo: {
      heading: 'Sehen Sie selbst: drei Server, Schritt für Schritt.',
      intro:
        'Eine kurze Geschichte mit drei OBIE-Servern, zwei Angreifern und einem Störenfried. Gehen Sie sie in Ihrem eigenen Tempo durch und sehen Sie zu, wie jeder Server selbst entscheidet.',
      illustration:
        'Illustration: erfundene Server und Beispieladressen, keine Live-Daten aus dem Netzwerk.',
      settings:
        'Hier sperrt ein Server eine Adresse, wenn die Meldungen, denen er vertraut, zusammen einen Wert von 1,2 erreichen (seine Schwelle) und von mindestens zwei Meldern stammen (sein Quorum). Der Leitfaden zur Föderation empfiehlt das für drei bis fünf Server; ab Werk liegt die Schwelle bei 1,8. Eine Fail2Ban-Meldung hat eine Konfidenz von 0,8. Diese Server sperren in ihren Firewalls; ein neuer Server beobachtet nur (Beobachtungsmodus), bis sein Betreiber das Sperren einschaltet.',
      steps: [
        {
          title: 'Die Nachbarschaft stellt sich vor',
          caption:
            'Drei Server mit je einem anderen Betreiber: ein Webshop (A), ein Hochschullabor (B) und ein Homelab (C). Sie verbinden sich direkt, ohne zentralen Server. Jeder legt fest, wie sehr er den anderen vertraut, von 0 bis 1.',
          planned:
            'Peers werden heute von Hand eingetragen. Sie automatisch zu finden, ist geplant.',
        },
        {
          title: 'Der Bot trifft Server A',
          caption:
            'Ein Bot rät Passwörter, Server für Server, und beginnt bei A. Der eigene Log-Wächter von A (etwa Fail2Ban) sperrt ihn direkt: Zum Selbstschutz braucht A niemandes Erlaubnis. Weil ein Kollege sich mehrmals vertippt, wird auch eine Büroadresse gesperrt.',
        },
        {
          title: 'A teilt eine signierte Meldung',
          caption:
            'A schickt B und C eine kurze Meldung: die Adresse, was sie tat, wie oft und einen Fingerabdruck der Belege. Die Logs selbst bleiben auf A. Seine digitale Signatur beweist B und C, dass die Meldung von A stammt.',
        },
        {
          title: 'Eine Stimme reicht nicht',
          caption:
            'B und C speichern die Meldungen von A, sperren aber nicht. Jeder bewertet eine Meldung: Vertrauen in den Absender mal dessen Konfidenz (wie sicher er sich ist). Eine Meldung allein bleibt unter der Schwelle; jeder verlangt zwei unabhängige Melder.',
          note: 'Ein einzelner fehlerhafter oder kompromittierter Server darf niemals erreichen, dass eine Adresse überall gesperrt wird. Die Büroadresse zeigt, warum.',
        },
        {
          title: 'Der Bot zieht weiter zu Server B',
          caption:
            'Als Nächstes versucht es der Bot bei B. Die eigene Erkennung von B schlägt an, und B sperrt ihn direkt. Seine eigene Meldung und die frühere von A stimmen überein: zwei vertraute Stimmen.',
        },
        {
          title: 'C ist geschützt, bevor der Angriff ankommt',
          caption:
            'B teilt seine signierte Meldung mit A und C. Zwei unabhängige, vertraute Meldungen (von A und B) erreichen nun die Schwelle von C, also sperrt C den Bot. Klopft der Bot Minuten später bei C an, wird er abgewiesen.',
          planned:
            'Heute zählt das Quorum Server, nicht Organisationen. Eine Prüfung, ob die Melder aus verschiedenen Netzen stammen, ist geplant.',
        },
        {
          title: 'Der Scanner trifft nur Server C',
          caption:
            'Ein Web-Scanner tastet nur C ab. C sperrt und meldet ihn. A und B beobachten nur: Ein Melder reicht ihnen nicht, und jeder gewichtet C nach seinem eigenen Vertrauen. Jeder Server entscheidet selbst.',
        },
        {
          title: 'Jemand versucht, das Mesh zu missbrauchen',
          caption:
            'Ein unbekannter Teilnehmer überflutet die Server mit Meldungen, damit der Zahlungsdienst des Shops gesperrt wird. Niemand vertraut ihm: Seine Meldungen wiegen 0, egal wie viele. Und der Dienst steht auf der Schutzliste von A: nie gesperrt, was immer gemeldet wird.',
          planned:
            'Vertrauen, das mit der bisherigen Zuverlässigkeit eines Peers wächst oder schrumpft, ist geplant. Heute legt jeder Betreiber die Zahlen selbst fest.',
        },
        {
          title: 'Fehler lassen sich rückgängig machen',
          caption:
            'A erfährt: Die Büroadresse ist der gemeinsame Anschluss eines Kollegen. A zieht seine Meldung per signiertem Widerruf zurück und hebt die Sperre auf. B und C verwerfen die Meldung automatisch. Sperren enden auch von selbst: Die einstündige Scanner-Sperre ist abgelaufen.',
          planned:
            'Ein Weg, über den der Inhaber einer gesperrten Adresse Einspruch einlegen kann, ist geplant.',
        },
        {
          title: 'Zusammenfassung',
          caption:
            'Geteiltes Wissen, souveräne Durchsetzung: Server warnen einander früh, und jeder Server entscheidet weiterhin selbst, was er sperrt.',
        },
      ],
      servers: {
        a: {
          name: 'Server A',
          short: 'A',
          operator: 'Webshop',
          safetyList: 'eigene Netze, Zahlungsdienst',
        },
        b: {
          name: 'Server B',
          short: 'B',
          operator: 'Hochschullabor',
          safetyList: 'eigene Netze, Campusnetz',
        },
        c: {
          name: 'Server C',
          short: 'C',
          operator: 'Homelab',
          safetyList: 'eigene Netze, Heimnetz',
        },
      },
      rogue: { name: 'Unbekannter Teilnehmer', short: 'R' },
      subjects: {
        bot: { name: 'Passwort-Bot', short: 'Bot' },
        office: { name: 'Büroadresse', short: 'Büro' },
        scanner: { name: 'Web-Scanner', short: 'Scanner' },
        payment: { name: 'Zahlungsdienst', short: 'Zahlung' },
      },
      states: {
        unknown: 'Unbekannt',
        watching: 'Beobachtet, nicht gesperrt',
        blocked: 'Gesperrt',
        safe: 'Nie gesperrt',
      },
      causes: {
        none: 'keine Meldungen',
        'below-bar': 'unter der Schwelle',
        agreement: 'genug vertraute Melder sind sich einig',
        'own-detection': 'eigene Erkennung',
        'safety-list': 'auf der Schutzliste',
      },
      labels: {
        trusts: 'Vertraut',
        anyoneElse: 'alle anderen {weight}',
        safetyList: 'Schutzliste',
        score: 'Wert {score} (nötig: {threshold})',
        reporters: '{count} von {quorum} Meldern',
        changed: 'Geändert',
        turnedAway: 'abgewiesen',
        copies: '×{n}',
        planned: 'Geplant',
        note: 'Warum',
      },
      controls: {
        label: 'Steuerung der Demo',
        restart: 'Neu starten',
        previous: 'Zurück',
        next: 'Weiter',
        play: 'Abspielen',
        pause: 'Pause',
        steps: 'Schritte',
        stepOf: 'Schritt {n} von {total}',
        goTo: 'Schritt {n}: {title}',
        announcement: 'Schritt {n} von {total}: {title}. {caption}',
        transcript: 'Alle zehn Schritte als Text',
        servers: 'Was jeder Server entscheidet',
      },
      report: {
        heading: 'Was {server} teilt',
        fields: {
          address: 'Adresse',
          reason: 'Was sie tat',
          events: 'Wie oft',
          fingerprint: 'Beleg',
          suggestion: 'Empfiehlt',
          confidence: 'Konfidenz',
          signature: 'Signatur',
        },
        reasons: { password_bruteforce: 'Passwörter raten', web_scan: 'Websites abtasten' },
        eventCount: '{n} fehlgeschlagene Anmeldungen',
        fingerprintNote:
          'ein Fingerabdruck der Logzeilen; aus ihm lassen sich die Zeilen nicht zurückgewinnen',
        suggestionValue: 'für {duration} sperren',
        signatureValue: 'Ed25519, geprüft von {receivers}',
        keptHeading: 'Was auf {server} bleibt',
        kept: ['die Logzeilen selbst', 'Benutzernamen und Passwörter', 'Kundendaten'],
      },
      recap: {
        takeaways: [
          'Jeder Server schützt zuerst sich selbst.',
          'Durch das Teilen können andere früher handeln.',
          'Niemand kann einem anderen befehlen, etwas zu sperren.',
        ],
        actions: [
          { label: 'Auf GitHub ansehen', href: LINKS.repository },
          { label: 'Loslegen', href: sectionHref('de', 'get-started') },
        ],
      },
    },
    nextStep: {
      label: 'Die allgemeinverständliche Einführung lesen: was OBIE ist, in fünf Minuten',
      href: LINKS.introduction,
    },
  },
  principles: {
    id: 'principles',
    label: 'Prinzipien',
    heading: 'Regeln, die OBIE zu einem Schild machen, nicht zu einer Waffe.',
    hook: 'Zehn Prinzipien, und das erste lautet: Belege vor Autorität.',
    cards: [
      {
        title: 'Belege vor Autorität',
        text: 'Vertrauen entsteht durch Daten, die Sie prüfen können, nicht durch ein Abzeichen. Jede Meldung ist signiert, Sie wissen also immer, wer sie geschickt hat.',
      },
      {
        title: 'Lokale Souveränität',
        text: 'Ihr Server trifft seine eigenen Entscheidungen. Meldungen anderer sind Ratschläge, niemals Befehle.',
      },
      {
        title: 'Kein zentraler Ausschalter',
        text: 'Es gibt keinen zentralen Server, der OBIE abschalten oder allen vorschreiben kann, was sie sperren.',
      },
      {
        title: 'Datenschutz als Grundeinstellung',
        text: 'Server teilen die Adressen von Angreifern. Die Identität Ihrer Nutzer oder Ihre unbearbeiteten Logs teilen sie nie.',
      },
      {
        title: 'Keine Tokens, keine Spekulation',
        text: 'Es gibt keine Kryptowährung und nichts zu handeln. Sie machen mit, weil gemeinsame Abwehr auch Sie schützt.',
      },
      {
        title: 'Praktisch einsetzbar',
        text: 'Wenn eine fähige Fachkraft es nicht an einem Wochenende in Betrieb nehmen kann, ist es Forschung, nicht Produktion.',
      },
    ],
    nextStep: { label: 'Alle zehn Prinzipien lesen', href: LINKS.manifesto },
  },
  status: {
    id: 'status',
    label: 'Status',
    heading: 'Wo OBIE heute steht.',
    intro:
      'Version 0.1 funktioniert im Code von Anfang bis Ende: Ein Server macht aus den Sperren von Fail2Ban signierte Meldungen, teilt sie mit den Peers, die Sie auswählen, entscheidet selbst und sperrt in seiner Firewall, sobald Sie das Sperren einschalten. Das erste Release ist noch nicht veröffentlicht, es gibt noch kein öffentliches Mesh zum Mitmachen, und eine unabhängige Sicherheitsprüfung hat noch nicht stattgefunden. Hier steht, was der Code heute kann und was als Nächstes kommt.',
    groups: [
      {
        state: 'available',
        label: 'Verfügbar',
        summary: 'Heute im Code',
        items: [
          {
            title: 'Signierte Meldungen',
            text: 'Jede Meldung trägt die Signatur ihres Servers. Das Format hat eine öffentliche Spezifikation, mit Testdaten für andere Implementierungen.',
          },
          {
            title: 'Meldungen aus Fail2Ban',
            text: 'Eine zusätzliche Zeile in einem Fail2Ban-Jail macht aus seinen Sperren signierte Meldungen. Die Logzeilen bleiben auf Ihrem Server.',
          },
          {
            title: 'Teilen mit Peers Ihrer Wahl',
            text: 'Server verbinden sich direkt mit den Peers auf Ihrer Liste. Ungültige Meldungen werden verworfen, und jeder Absender darf nur begrenzt viele Meldungen schicken.',
          },
          {
            title: 'Entscheidungen auf jedem Server',
            text: 'Vertrauensgewichte pro Peer, eine Mindestzahl übereinstimmender Peers und ein Schwellenwert. Der Knoten erklärt, warum er eine Adresse sperrt oder nicht.',
          },
          {
            title: 'Schutzliste und eigene Vorgaben',
            text: 'Ihre eigenen Adressen, interne Netze und Ihre Peers werden nie gesperrt. Ergänzen Sie die Netze, auf die Sie angewiesen sind, und erlauben oder sperren Sie jede andere Adresse selbst.',
          },
          {
            title: 'Erst beobachten, dann sperren',
            text: 'Ein neuer Server zeigt nur, was er sperren würde. Im Sperrmodus (enforce mode) sperrt er in seiner eigenen nftables-Tabelle, und jede Sperre läuft ab.',
          },
          {
            title: 'Einrichtung, Selbsttest und Webkonsole',
            text: 'Ein Einrichtungsassistent, ein Selbsttest, der warnt, bevor Sie sich aussperren könnten, eine optionale Webkonsole, Metriken und ein Audit-Log.',
          },
          {
            title: 'Eine Sandbox zum Ausprobieren',
            text: 'Vier Knoten in Docker auf Ihrem eigenen Rechner, mit einer Anleitung Schritt für Schritt. Dabei wird nie etwas gesperrt.',
          },
        ],
      },
      {
        state: 'in-progress',
        label: 'In Arbeit',
        summary: 'Wird jetzt vorbereitet und entworfen',
        items: [
          {
            title: 'Das erste Release',
            text: 'Release-Pakete, ein Installationsprogramm, ein abgesicherter Dienst und ein Container-Image sind fertig und werden bei jeder Änderung getestet. Veröffentlicht ist das Release noch nicht.',
          },
          {
            title: 'Zuverlässige Zustellung',
            text: 'Simulationen mit 1.000 bis 10.000 Servern zeigen: In einem großen Mesh um wenige Knotenpunkte kommt etwa jede sechste Meldung nie an, und ein Server, der offline war, verpasst, was in der Zwischenzeit verschickt wurde. Abhilfe wird entworfen.',
          },
          {
            title: 'Schutz vor Fluten und Fälschungen',
            text: 'In denselben Simulationen verdrängte eine Flut von Absendern, denen niemand vertraut, die vertrauenswürdigen Meldungen aus einem vollen Speicher, und gefälschte Nachrichten hielten einen Widerruf von den meisten Servern fern. Gegenmaßnahmen werden entworfen.',
          },
        ],
      },
      {
        state: 'planned',
        label: 'Geplant',
        summary: 'Spätere Versionen',
        items: [
          {
            title: 'Automatische Peer-Suche',
            text: 'Andere OBIE-Server finden, ohne jeden einzeln von Hand einzutragen.',
          },
          {
            title: 'Erarbeitete Reputation',
            text: 'Vertrauen in einen Peer, das wächst oder schrumpft, je nachdem, wie zutreffend sich seine Meldungen erweisen.',
          },
          {
            title: 'Vielfaltsprüfungen',
            text: 'Nur handeln, wenn Meldungen aus mehreren unabhängigen Netzen und Organisationen kommen.',
          },
          {
            title: 'Einsprüche',
            text: 'Ein Weg, über den der Inhaber einer gesperrten Adresse eine Überprüfung verlangen kann.',
          },
          {
            title: 'Sperren mit eBPF',
            text: 'Sehr schnelles Filtern im Linux-Kernel bei schweren Angriffen.',
          },
        ],
      },
    ],
    details: {
      label: 'Was Version 0.1 schon kann, was noch nicht und was sie voraussetzt',
      href: LINKS.capabilities,
    },
    nextStep: { label: 'Den Fortschritt auf GitHub verfolgen', href: LINKS.repository },
  },
  getStarted: {
    id: 'get-started',
    label: 'Loslegen',
    heading: 'In drei Schritten ausprobieren.',
    intro:
      'OBIE ist für Fachleute gebaut, die ihre eigenen Linux-Server betreiben. Probieren Sie es zuerst auf Ihrem eigenen Rechner aus, dann auf einem Server.',
    steps: [
      {
        title: 'In der Sandbox ausprobieren',
        text: 'Starten Sie in einer Kopie des Quellcodes vier OBIE-Knoten in Docker auf Ihrem eigenen Rechner. Melden Sie einen Angriff, sehen Sie zu, wie die anderen entscheiden, und fragen Sie sie nach dem Warum. Dabei wird nie etwas gesperrt, und Root-Rechte brauchen Sie nicht.',
        code: 'cd packaging/sandbox && ./sandbox up',
      },
      {
        title: 'Installieren und beobachten',
        text: 'Installieren Sie OBIE auf einem Linux-Server mit systemd und beantworten Sie die fünf Fragen des Einrichtungsassistenten. Der Knoten startet im Beobachtungsmodus: Er zeigt, was er sperren würde, und sperrt nichts.',
        code: 'sudo obied setup',
      },
      {
        title: 'Prüfen, verbinden, dann sperren',
        text: 'Der Selbsttest sagt, was stimmt und was als Nächstes zu beheben ist. Verbinden Sie Fail2Ban und einen Peer, dem Sie vertrauen. Schalten Sie das Sperren erst ein, wenn Sie sicher sind, dass Sie sich nicht selbst aussperren können.',
        code: 'sudo obied self-check',
      },
    ],
    note: 'Die Release-Pakete mit dem Installationsprogramm kommen mit dem ersten Release, das noch nicht veröffentlicht ist. Bis dahin wird OBIE aus dem Quellcode gebaut, mit Go 1.26 oder neuer; die Sandbox erledigt das für Sie.',
    quickStart: { label: 'Die Sandbox Schritt für Schritt', href: LINKS.sandbox },
    project: {
      linksLabel: 'OBIE auf GitHub',
      links: [
        { label: 'Repository', href: LINKS.repository },
        { label: 'Erste Schritte', href: LINKS.quickStart },
        { label: 'Protokollspezifikation', href: LINKS.spec },
        { label: 'Issues für den Einstieg', href: LINKS.goodFirstIssues },
      ],
      statsCaption: 'Das Projekt auf GitHub',
      stars: 'Sterne',
      latestRelease: 'Neuestes Release',
      noRelease: 'Noch keines',
      lastActivity: 'Letzter Commit',
    },
    nextStep: {
      label: 'Die Schritt-für-Schritt-Anleitung auf GitHub öffnen',
      href: LINKS.quickStart,
    },
  },
  founder: {
    id: 'founder',
    label: 'Gründer',
    heading: 'Wer OBIE ins Leben gerufen hat.',
    name: 'Markus Niewerth',
    role: 'Gründer von OBIE · Softwarearchitekt · Geschäftsführer, Cloudwerks Technology GmbH',
    bio: 'Markus Niewerth entwickelt seit 15 Jahren Softwaresysteme, die Komplexität verringern. Als Gründer der Cloudwerks Technology GmbH entwirft und baut er ihre Produkte selbst, darunter QuickSelect, Krisis und OBIE, und arbeitet parallel als Softwarearchitekt an Automotive-Plattformen (Software-defined Vehicle, Android Automotive OS). OBIE startete er, nachdem er für ein Projekt Sicherheits-SDKs auf Crowdsourcing-Basis evaluiert hatte und dabei auf das gestoßen war, was er die Zentralisierungsfalle nennt.',
    // TODO(operator): Porträtfoto. Das Foto in public/founder/ ablegen, `src`
    // darauf zeigen lassen und es in `alt` beschreiben; beim Platzhalter bleibt `alt` leer.
    photo: { src: FOUNDER_AVATAR_PLACEHOLDER, alt: '' },
    topicsHeading: 'Vorgeschlagene Vortragsthemen',
    topicsNote:
      'Vorschläge, abgeleitet aus den Prinzipien von OBIE. Ein eigenes Thema können Sie im Formular unten vorschlagen.',
    // TODO(operator): Vortragsthemen bestätigen (Vorschläge aus den Themen von
    // OBIE; bisher ist keines davon belegt).
    topics: [
      'Die Zentralisierungsfalle: gemeinsame Abwehr ohne zentrale Instanz',
      'Belege vor Autorität: ein Protokoll zum Teilen von Bedrohungsinformationen entwerfen, dem man nicht vertrauen muss',
      'Lokale Souveränität in der Praxis: vertrauensgewichtete Entscheidungen und Allowlists, die immer Vorrang haben',
      'Langweilig robust: Sicherheitssoftware, die eine fähige Fachkraft an einem Wochenende in Betrieb nehmen kann',
    ],
    linksLabel: 'Profile',
    links: [
      { label: 'LinkedIn', href: 'https://www.linkedin.com/in/niewerth/' },
      { label: 'GitHub', href: 'https://github.com/MNCloudwerksTechnology' },
    ],
    invite: { label: 'Markus als Redner einladen', href: sectionHref('de', CONTACT_ID) },
    nextStep: { label: 'Eine Frage auf GitHub stellen', href: LINKS.issues },
  },
  contact: {
    id: CONTACT_ID,
    label: 'Kontakt',
    heading: 'Markus als Redner einladen oder Kontakt aufnehmen.',
    intro:
      'Für Vorträge, Workshops, Interviews, Forschungskooperationen und andere Fragen zu OBIE. Ihre Nachricht geht direkt an Markus Niewerth.',
    form: {
      typeLegend: 'Worum geht es in Ihrer Anfrage?',
      types: [
        { value: 'talk', label: 'Vortrag' },
        { value: 'workshop', label: 'Workshop' },
        { value: 'interview', label: 'Interview oder Presse' },
        { value: 'collaboration', label: 'Zusammenarbeit oder Forschung' },
        { value: 'other', label: 'Sonstiges' },
      ],
      eventLegend: 'Zur Veranstaltung',
      fields: {
        name: { label: 'Ihr Name' },
        email: { label: 'E-Mail-Adresse', hint: 'Wird nur verwendet, um Ihnen zu antworten.' },
        organisation: { label: 'Organisation' },
        eventDate: { label: 'Datum' },
        eventLocation: { label: 'Ort', hint: 'Eine Stadt, ein Veranstaltungsort oder „online“.' },
        audienceSize: { label: 'Erwartete Teilnehmerzahl', hint: 'Anzahl der Personen.' },
        message: { label: 'Nachricht', hint: 'Mindestens 20 Zeichen.' },
      },
      optional: '(optional)',
      consent: {
        before: 'Ich habe die ',
        link: { label: 'Datenschutzerklärung', href: PAGE_PATHS.privacy.de },
        after:
          ' gelesen und verstehe, dass meine Anfrage gespeichert wird, damit sie beantwortet werden kann.',
      },
      honeypot: 'Dieses Feld bitte leer lassen',
      submit: 'Anfrage senden',
      sending: 'Wird gesendet …',
      invalid: 'Bitte prüfen Sie die markierten Felder.',
      success: {
        heading: 'Anfrage gesendet.',
        text: 'Danke, Markus meldet sich innerhalb weniger Tage bei Ihnen.',
      },
      error: {
        heading: 'Ihre Anfrage wurde nicht gesendet.',
        text: 'Etwas ist schiefgelaufen, bei uns oder bei der Verbindung. Ihre Eingaben sind noch da, bitte versuchen Sie es erneut.',
        expired:
          'Das Formular war lange geöffnet und musste aktualisiert werden. Ihre Eingaben sind noch da, bitte senden Sie es erneut.',
        rateLimited:
          'Aus Ihrem Netzwerk kamen zu viele Anfragen. Bitte versuchen Sie es später erneut.',
        retry: 'Erneut versuchen',
      },
      messages: {
        typeRequired: 'Bitte wählen Sie, worum es in Ihrer Anfrage geht.',
        nameRequired: 'Bitte geben Sie Ihren Namen ein.',
        emailRequired: 'Bitte geben Sie Ihre E-Mail-Adresse ein.',
        emailInvalid: 'Bitte geben Sie eine gültige E-Mail-Adresse ein.',
        messageRequired: 'Bitte geben Sie eine Nachricht ein.',
        messageLength: 'Bitte schreiben Sie zwischen 20 und 5.000 Zeichen.',
        maxLength: 'Bitte verwenden Sie höchstens {max} Zeichen.',
        singleLine: 'Bitte verwenden Sie nur eine Zeile.',
        controlCharacters: 'Bitte entfernen Sie spezielle Steuerzeichen.',
        dateInFuture: 'Bitte wählen Sie ein Datum in der Zukunft.',
        dateFormat: 'Bitte geben Sie das Datum im Format JJJJ-MM-TT ein.',
        positiveNumber: 'Bitte geben Sie eine positive Zahl ein.',
        wholeNumber: 'Bitte geben Sie eine ganze Zahl ein.',
        maxAudience: 'Bitte geben Sie höchstens 1.000.000 ein.',
        consentRequired: 'Bitte bestätigen Sie, dass Sie die Datenschutzerklärung gelesen haben.',
      },
    },
    nextStep: {
      label: 'Lieber öffentlich fragen? Eröffnen Sie ein Issue auf GitHub',
      href: LINKS.issues,
    },
  },
  faq: {
    id: 'faq',
    label: 'FAQ',
    heading: 'Ehrliche Antworten.',
    items: [
      {
        question: 'Ist OBIE kostenlos?',
        answer:
          'Ja. Code und Spezifikation sind Open Source unter der MIT-Lizenz. Es gibt keine Tokens und keine Kryptowährung.',
      },
      {
        question:
          'Kann ein böswilliger Peer erreichen, dass eine Adresse auf meinem Server gesperrt wird?',
        answer:
          'Nicht allein. In Version 0.1 reagiert Ihr Server nur auf Meldungen von Quellen, die Sie selbst als vertrauenswürdig ausgewählt haben: standardmäßig erst, wenn mindestens zwei davon dieselbe Adresse melden (Ihr eigener Server zählt als eine) und ihre gemeinsame Konfidenz ausreicht, und niemals gegen Ihre Schutzliste. Meldungen über private und interne Netzwerkadressen werden von vornherein abgelehnt. Peers, denen Sie vertrauen, könnten sich trotzdem bei einer falschen Meldung einig sein. Deshalb wählen Sie sie sorgfältig aus und können im Beobachtungsmodus beginnen. Automatisch erarbeitetes Vertrauen ist geplant.',
      },
      {
        question: 'Welche Daten verlassen meinen Server?',
        answer:
          'Nur signierte Meldungen in dem Format, das die Spezifikation festlegt: die angreifende Adresse, der angegriffene Dienst, wie viele Ereignisse gesehen wurden, ein Begründungscode, ob ein Honeypot den Angriff gesehen hat, die vorgeschlagene Maßnahme, ein Konfidenzwert und wie lange die Maßnahme gelten soll. Optional sind ein Fingerabdruck der Logzeilen, der belegt, was Sie gesehen haben, ohne es offenzulegen, Codes für die Angriffstechnik (MITRE-ATT&CK-IDs) und die Nummer Ihres Netzes (seine ASN). Ein Server kann seine eigene Meldung auch mit einem signierten Widerruf zurückziehen, der einen Begründungscode trägt. Das Format hat keinen Platz für Logs, Benutzernamen, Passwörter oder Freitext. Jeder Knoten, der mit Ihrem Mesh verbunden ist, erhält Ihre Meldungen, ob Sie ihm vertrauen oder nicht, und sieht die Adresse Ihres Servers und seine öffentliche OBIE-ID.',
      },
      {
        question: 'Brauche ich Fail2Ban?',
        answer:
          'Fail2Ban ist das erste Erkennungswerkzeug, mit dem OBIE zusammenarbeitet: Eine zusätzliche Zeile in einem Jail macht aus seinen Sperren signierte Meldungen. Ein Server braucht kein eigenes Erkennungswerkzeug, um auf Meldungen von Peers zu reagieren, denen er vertraut, und jedes Werkzeug, das einen Befehl ausführen kann, kann eine Adresse melden. Fertige Unterstützung für Honeypots (Köderserver, die Angreifer anlocken) ist geplant.',
      },
      {
        question: 'Ist OBIE bereit für den Produktivbetrieb?',
        answer:
          'Noch nicht. Version 0.1 funktioniert von Anfang bis Ende: Sie meldet, teilt, entscheidet und sperrt, sobald Sie das Sperren einschalten. Aber das erste Release ist noch nicht veröffentlicht, eine unabhängige Sicherheitsprüfung hat noch nicht stattgefunden, und Simulationen haben Schwächen gefunden, die noch zu beheben sind (siehe „Status“). Wenn Sie OBIE auf einem Server ausprobieren, beginnen Sie im Beobachtungsmodus, der nichts sperrt, und lesen Sie, was OBIE noch nicht kann.',
      },
      {
        question: 'Gibt es einen zentralen Server, der OBIE abschalten kann?',
        answer:
          'Nein. Server verbinden sich direkt miteinander. Es gibt keinen zentralen Server, kein Konto und keinen Ausschalter.',
      },
      {
        question: 'Wer steht dahinter?',
        answer:
          'OBIE wird offen auf GitHub entwickelt, mit einer öffentlichen Spezifikation und Code unter MIT-Lizenz. Das Unternehmen, das es initiiert hat, ist in der Fußzeile genannt, und der Abschnitt über den Gründer weiter oben stellt die Person vor, die es gestartet hat. Jeder kann den Code lesen, Fehler melden und etwas beitragen.',
      },
    ],
    nextStep: { label: 'Eine eigene Frage auf GitHub stellen', href: LINKS.issues },
  },
  footer: {
    tagline: 'OBIE: Shared Intelligence, Sovereign Enforcement.',
    github: { label: 'Auf GitHub ansehen', href: LINKS.repository },
    links: [
      { label: 'Was ist OBIE?', href: LINKS.introduction },
      { label: 'Was OBIE kann und was nicht', href: LINKS.capabilities },
      { label: 'Spezifikation', href: LINKS.spec },
      { label: 'Sicherheitsrichtlinie', href: LINKS.securityPolicy },
      { label: 'Markus als Redner einladen', href: sectionHref('de', CONTACT_ID) },
      { label: 'Impressum', href: PAGE_PATHS.impressum.de },
      { label: 'Datenschutz', href: PAGE_PATHS.privacy.de },
      { label: 'MIT-Lizenz', href: LINKS.licence },
    ],
    attribution: {
      text: 'Ein offenes Protokoll, initiiert von der Cloudwerks Technology GmbH.',
      href: LINKS.cloudwerks,
    },
    licence: 'MIT-Lizenz · © 2026 Cloudwerks Technology GmbH',
  },
};
