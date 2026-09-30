import { ANALYTICS_HOST, CONSENT_STORAGE_KEY } from '../core/analytics/analytics.config';
import { LegalContent } from './legal-content.model';
import { LINKS } from './landing.content';
import {
  ANALYTICS_RAW_DATA_RETENTION_MONTHS,
  INQUIRY_MAILBOX_RETENTION_YEARS,
  INQUIRY_RETENTION_MONTHS,
  OPERATOR_ADDRESS,
  OPERATOR_EMAIL,
  OPERATOR_PHONE,
  OPERATOR_PHONE_HREF,
  SERVER_LOG_RETENTION_DAYS,
} from './operator';

// Impressum (§ 5 DDG, § 18 MStV) and Datenschutzerklärung (Art. 13 DSGVO) in
// German. Same facts, sections and blocks as LEGAL_CONTENT_EN in
// legal.content.ts; change both together. The German headings are the legal
// terms themselves, so neither page nor section names a separate German term.

/** Street, town and country, as the German pages state the address. */
const ADDRESS_DE = [...OPERATOR_ADDRESS, 'Deutschland'];

/** How long inquiries are kept, as the German policy says it. */
export const INQUIRY_RETENTION_DE = `${INQUIRY_RETENTION_MONTHS} Monate`;

/** How long the web server keeps its access logs, as the German policy says it. */
export const SERVER_LOG_RETENTION_DE = `${SERVER_LOG_RETENTION_DAYS} Tage`;

/** German legal pages: LEGAL_CONTENT_EN (legal.content.ts) in German. */
export const LEGAL_CONTENT_DE: LegalContent = {
  reviewPending: true,
  reviewNotice: {
    heading: 'Entwurf: Prüfung durch den Betreiber ausstehend',
    text: 'Diese Seite muss vom Betreiber geprüft werden, bevor die Website online geht. Bis dahin handelt es sich um einen Entwurf.',
  },
  impressum: {
    meta: {
      title: 'Impressum · OBIE',
      description:
        'Impressum der OBIE-Website gemäß § 5 DDG: Anbieter, Kontakt, Handelsregistereintrag und Umsatzsteuer-Identifikationsnummer.',
    },
    heading: 'Impressum',
    sections: [
      {
        id: 'provider',
        heading: 'Angaben gemäß § 5 DDG',
        blocks: [
          {
            kind: 'facts',
            items: [
              { term: 'Anbieter', lines: ['Cloudwerks Technology GmbH'] },
              { term: 'Anschrift', lines: ADDRESS_DE },
              { term: 'Vertreten durch', lines: ['Markus Niewerth, Geschäftsführer'] },
            ],
          },
        ],
      },
      {
        id: 'contact',
        heading: 'Kontakt',
        blocks: [
          {
            kind: 'facts',
            items: [
              { term: 'Telefon', lines: [OPERATOR_PHONE], href: OPERATOR_PHONE_HREF },
              { term: 'E-Mail', lines: [OPERATOR_EMAIL], href: `mailto:${OPERATOR_EMAIL}` },
            ],
          },
        ],
      },
      {
        id: 'register',
        heading: 'Eintragung im Handelsregister',
        blocks: [
          {
            kind: 'facts',
            items: [
              { term: 'Registergericht', lines: ['Amtsgericht Gelsenkirchen'] },
              { term: 'Registernummer', lines: ['HRB 17839'] },
            ],
          },
        ],
      },
      {
        id: 'vat-id',
        heading: 'Umsatzsteuer-Identifikationsnummer gemäß § 27a UStG',
        blocks: [{ kind: 'facts', items: [{ term: 'USt-IdNr.', lines: ['DE363640900'] }] }],
      },
      {
        id: 'responsible',
        heading: 'Verantwortlich für den Inhalt nach § 18 Abs. 2 MStV',
        blocks: [
          {
            kind: 'facts',
            items: [{ term: 'Verantwortlich', lines: ['Markus Niewerth', ...OPERATOR_ADDRESS] }],
          },
        ],
      },
      {
        id: 'dispute-resolution',
        heading: 'Verbraucherstreitbeilegung',
        blocks: [
          {
            kind: 'paragraph',
            text: 'Wir sind weder bereit noch verpflichtet, an Streitbeilegungsverfahren vor einer Verbraucherschlichtungsstelle teilzunehmen.',
          },
        ],
      },
      {
        id: 'liability-content',
        heading: 'Haftung für Inhalte',
        blocks: [
          {
            kind: 'paragraph',
            text: 'Die Inhalte dieser Website erstellen wir mit größter Sorgfalt und nach bestem Wissen. Für die Richtigkeit, Vollständigkeit und Aktualität der Inhalte können wir jedoch keine Gewähr übernehmen. Als Diensteanbieter sind wir für eigene Inhalte auf diesen Seiten nach den allgemeinen Gesetzen verantwortlich. Wir sind jedoch nicht verpflichtet, übermittelte oder gespeicherte fremde Informationen zu überwachen oder nach Umständen zu forschen, die auf eine rechtswidrige Tätigkeit hinweisen. Verpflichtungen zur Entfernung oder Sperrung der Nutzung von Informationen nach den allgemeinen Gesetzen bleiben hiervon unberührt.',
          },
          {
            kind: 'paragraph',
            text: 'Eine diesbezügliche Haftung ist jedoch erst ab dem Zeitpunkt der Kenntnis einer konkreten Rechtsverletzung möglich. Sobald uns eine solche Rechtsverletzung bekannt wird, entfernen wir die betreffenden Inhalte unverzüglich.',
          },
        ],
      },
      {
        id: 'liability-links',
        heading: 'Haftung für Links',
        blocks: [
          {
            kind: 'paragraph',
            text: 'Diese Website enthält Links zu externen Websites Dritter, zum Beispiel zu GitHub, auf deren Inhalte wir keinen Einfluss haben. Deshalb können wir für diese fremden Inhalte auch keine Haftung übernehmen; für die Inhalte der verlinkten Seiten ist stets der jeweilige Anbieter oder Betreiber der Seiten verantwortlich.',
          },
          {
            kind: 'paragraph',
            text: 'Wir haben die verlinkten Seiten zum Zeitpunkt der Verlinkung auf mögliche Rechtsverstöße überprüft und dabei keine festgestellt. Eine permanente inhaltliche Kontrolle der verlinkten Seiten ist ohne konkrete Anhaltspunkte einer Rechtsverletzung jedoch nicht zumutbar. Sobald uns eine Rechtsverletzung bekannt wird, entfernen wir den betreffenden Link unverzüglich.',
          },
        ],
      },
      {
        id: 'copyright',
        heading: 'Urheberrecht',
        blocks: [
          {
            kind: 'paragraph',
            text: 'Die Inhalte und Werke auf dieser Website unterliegen dem deutschen Urheberrecht. Soweit eine Lizenz nichts anderes bestimmt, bedürfen die Vervielfältigung, Bearbeitung, Verbreitung und jede Art der Verwertung außerhalb der Grenzen des Urheberrechts der vorherigen schriftlichen Zustimmung des jeweiligen Urhebers. Quellcode und Spezifikation von OBIE sind unter der ',
            link: { label: 'MIT-Lizenz', href: LINKS.licence },
            after: ' veröffentlicht, die ihre Nutzung nach Maßgabe ihrer Bedingungen erlaubt.',
          },
          {
            kind: 'paragraph',
            text: 'Soweit die Inhalte auf dieser Website nicht von uns erstellt wurden, werden die Urheberrechte Dritter beachtet. Sollten Sie trotzdem auf eine Urheberrechtsverletzung aufmerksam werden, bitten wir um einen entsprechenden Hinweis. Sobald uns eine solche Rechtsverletzung bekannt wird, entfernen wir die betreffenden Inhalte unverzüglich.',
          },
        ],
      },
    ],
  },
  privacy: {
    meta: {
      title: 'Datenschutzerklärung · OBIE',
      description:
        'Datenschutz auf der OBIE-Website: keine Cookies, Besucherstatistik nur mit Ihrer Einwilligung, was das Kontaktformular speichert und wie lange.',
    },
    heading: 'Datenschutzerklärung',
    updated: 'Stand: 30. September 2026',
    intro:
      'Mit dieser Datenschutzerklärung informieren wir Sie gemäß Art. 13 der Datenschutz-Grundverordnung (DSGVO) darüber, welche personenbezogenen Daten die Cloudwerks Technology GmbH („wir“) verarbeitet, wenn Sie diese Website besuchen oder uns eine Anfrage senden, zu welchem Zweck, auf welcher Rechtsgrundlage und wie lange. Begriffe wie „personenbezogene Daten“ und „Verarbeitung“ sind im Sinne von Art. 4 DSGVO zu verstehen.',
    sections: [
      {
        id: 'summary',
        heading: 'Kurzfassung',
        blocks: [
          {
            kind: 'list',
            items: [
              'Keine Cookies. Diese Website setzt keine Cookies. In Ihrem Browser speichert sie ausschließlich Ihre Antwort auf die Frage nach der Besucherstatistik, sobald Sie diese beantwortet haben.',
              `Besucherstatistik nur mit Ihrer Einwilligung. Wenn Sie zustimmen, erfassen wir Besuche mit Matomo auf unserem eigenen Statistikserver ${ANALYTICS_HOST}, ohne Cookies und ohne Weitergabe von Daten an Dritte. Ihre Einwilligung können Sie jederzeit unter „Datenschutz-Einstellungen“ am Ende jeder Seite widerrufen.`,
              'Keine Anfragen an Dritte. Schriftarten, Bilder und Skripte werden vom eigenen Server dieser Website geladen; Ihr Browser nimmt keine Verbindung zu Google Fonts, Content-Delivery-Netzwerken oder sonstigen Dritten auf. Nur wenn Sie der Besucherstatistik zustimmen, verbindet er sich zusätzlich mit unserem Statistikserver.',
              'Keine Werbung und keine Profile. Wir nutzen keine Werbedienste und führen Ihren Besuch nicht mit anderen Daten zusammen.',
              'Abgesehen von den Server-Logfiles, der Besucherstatistik, sofern Sie ihr zustimmen, und dem unten beschriebenen Schutz vor Missbrauch erhalten wir nur, was Sie in das Kontaktformular eingeben, und verwenden es ausschließlich, um Ihnen zu antworten.',
            ],
          },
        ],
      },
      {
        id: 'controller',
        heading: 'Verantwortlicher',
        blocks: [
          {
            kind: 'facts',
            items: [
              { term: 'Verantwortlicher', lines: ['Cloudwerks Technology GmbH', ...ADDRESS_DE] },
              { term: 'Vertreten durch', lines: ['Markus Niewerth, Geschäftsführer'] },
              { term: 'E-Mail', lines: [OPERATOR_EMAIL], href: `mailto:${OPERATOR_EMAIL}` },
              { term: 'Telefon', lines: [OPERATOR_PHONE], href: OPERATOR_PHONE_HREF },
            ],
          },
          {
            kind: 'paragraph',
            text: 'Wir sind gesetzlich nicht verpflichtet, einen Datenschutzbeauftragten zu benennen, und haben keinen benannt.',
          },
        ],
      },
      {
        id: 'hosting',
        heading: 'Hosting',
        blocks: [
          {
            kind: 'paragraph',
            text: 'Diese Website wird einschließlich ihrer Datenbank auf unserem eigenen Server betrieben. Kein Hosting-Anbieter verarbeitet diese Daten in unserem Auftrag.',
          },
        ],
      },
      {
        id: 'server-logs',
        heading: 'Server-Logfiles',
        blocks: [
          {
            kind: 'paragraph',
            text: 'Beim Aufruf einer Seite erfasst der Webserver automatisch die Daten, die Ihr Browser mit jeder Anfrage übermittelt:',
          },
          {
            kind: 'list',
            items: [
              'die aufgerufene Seite oder Datei mit Datum und Uhrzeit',
              'den HTTP-Statuscode und die übertragene Datenmenge',
              'die verweisende Seite, sofern Ihr Browser sie übermittelt',
              'Browser und Betriebssystem (User-Agent)',
              'die IP-Adresse Ihres Endgeräts',
            ],
          },
          {
            kind: 'paragraph',
            text: `Wir benötigen diese Logfiles, um die Website sicher und zuverlässig zu betreiben, etwa um Angriffe zu erkennen und aufzuklären; darin liegt unser berechtigtes Interesse (Art. 6 Abs. 1 lit. f DSGVO). Die Logfiles werden nach höchstens ${SERVER_LOG_RETENTION_DE} gelöscht. Daten, die zur Beweissicherung eines konkreten Vorfalls erforderlich sind, werden bis zu dessen Klärung aufbewahrt. Die Website-Anwendung selbst schreibt kein Zugriffsprotokoll. Ihr eigenes Protokoll erfasst technische Ereignisse, etwa die Vorgangsnummer einer Anfrage, nicht aber Ihre IP-Adresse oder den Inhalt Ihrer Anfrage; nur wenn eine E-Mail nicht zugestellt werden kann, kann die dort protokollierte Fehlermeldung des Mailservers Ihre E-Mail-Adresse enthalten. Das Anwendungsprotokoll wird nicht länger aufbewahrt als die Server-Logfiles.`,
          },
        ],
      },
      {
        id: 'cookies',
        heading: 'Cookies und Speicherung im Browser',
        blocks: [
          {
            kind: 'paragraph',
            text: `Diese Website setzt keine Cookies. Sie speichert eine einzige Information auf Ihrem Endgerät: Sobald Sie die Frage nach der Besucherstatistik beantwortet haben, wird Ihre Antwort („granted“ für erteilt oder „denied“ für abgelehnt) im lokalen Speicher (Local Storage) Ihres Browsers unter dem Namen ${CONSENT_STORAGE_KEY} abgelegt, damit wir Sie nicht auf jeder Seite erneut fragen. Diese Speicherung ist unbedingt erforderlich, um Ihre Entscheidung zu beachten (§ 25 Abs. 2 Nr. 2 TDDDG). Sie enthält nichts außer Ihrer Antwort, wird nie an uns übermittelt und bleibt bestehen, bis Sie Ihre Entscheidung ändern oder die Browserdaten für diese Website löschen. Darüber hinaus und abgesehen von der Besucherstatistik, sofern Sie ihr zustimmen (siehe unten), speichert diese Website keine Informationen auf Ihrem Endgerät und greift auch nicht auf dort gespeicherte Informationen zu, soweit dies nicht technisch erforderlich ist, um die von Ihnen aufgerufene Seite bereitzustellen (§ 25 TDDDG). Wenn Sie zwischen hellem und dunklem Design wechseln, gilt diese Wahl nur für Ihren aktuellen Besuch und wird nicht gespeichert.`,
          },
          {
            kind: 'paragraph',
            text: 'Die Schriftarten (Inter und Source Code Pro) werden vom eigenen Server dieser Website ausgeliefert; eine Verbindung zu Google Fonts oder anderen Schriftartendiensten findet nicht statt. Inhalte anderer Anbieter wie Videos, Karten oder Social-Media-Schaltflächen binden wir nicht ein.',
          },
        ],
      },
      {
        id: 'analytics',
        heading: 'Webanalyse mit Matomo',
        blocks: [
          {
            kind: 'paragraph',
            text: `Mit Ihrer Einwilligung erfassen wir, wie diese Website genutzt wird: welche Seiten und Abschnitte gelesen werden, woher Besucher kommen und welchen Links sie folgen, damit wir die Website verbessern können. Dafür setzen wir Matomo ein, eine Open-Source-Software zur Webanalyse, die wir auf unserem eigenen Statistikserver ${ANALYTICS_HOST} betreiben. Es werden keine Daten an den Hersteller von Matomo oder andere Dritte übermittelt.`,
          },
          {
            kind: 'paragraph',
            text: `Bis Sie „Zustimmen“ wählen, wird nichts erfasst. Erst dann lädt Ihr Browser das Matomo-Skript von ${ANALYTICS_HOST} und übermittelt dorthin bei jedem Seitenaufruf folgende Angaben:`,
          },
          {
            kind: 'list',
            items: [
              'die aufgerufene Seite, ihren Titel und die Seite, von der Sie kommen (Referrer)',
              'Datum und Uhrzeit sowie die Dauer, für die die Seite geöffnet bleibt',
              'Links zu anderen Websites, denen Sie folgen',
              'ob Sie eine Anfrage gesendet haben (deren Art, niemals deren Inhalt) und ob Sie die Drei-Server-Demo gestartet und abgeschlossen haben',
              'Browser, Betriebssystem, Gerätetyp, Bildschirmauflösung und bevorzugte Sprache',
              'Ihre IP-Adresse, aus der Matomo Ihre ungefähre Herkunftsregion ermittelt',
            ],
          },
          {
            kind: 'paragraph',
            text: 'Matomo setzt auf dieser Website keine Cookies. Um die Seitenaufrufe eines Besuchs von denen anderer Besuche zu unterscheiden, bildet Matomo aus den oben genannten Daten eine Kennung, die sich täglich ändert; aus ihr erfahren wir nicht, wer Sie sind. Wir geben die Statistik nicht an Dritte weiter, führen sie nicht mit anderen Daten zusammen und werten sie nur in zusammengefasster Form aus.',
          },
          {
            kind: 'paragraph',
            text: 'Rechtsgrundlage: Ihre Einwilligung (Art. 6 Abs. 1 lit. a DSGVO; für das Speichern von Informationen auf Ihrem Endgerät und den Zugriff darauf § 25 Abs. 1 TDDDG). Die Einwilligung ist freiwillig: Ohne sie funktioniert die Website genauso, und es wird nichts erfasst. Sie können sie jederzeit mit Wirkung für die Zukunft widerrufen (Art. 7 Abs. 3 DSGVO): Wählen Sie dazu am Ende einer beliebigen Seite „Datenschutz-Einstellungen“ und anschließend „Ablehnen“.',
          },
          {
            kind: 'paragraph',
            text: `Speicherdauer: Matomo löscht die Rohdaten der Besuche nach ${ANALYTICS_RAW_DATA_RETENTION_MONTHS} Monaten. Zusammengefasste Berichte enthalten keine personenbezogenen Daten.`,
          },
        ],
      },
      {
        id: 'inquiries',
        heading: 'Kontaktformular',
        blocks: [
          {
            kind: 'paragraph',
            text: 'Wenn Sie uns eine Anfrage senden, etwa um Markus Niewerth zu einem Vortrag einzuladen, verarbeiten wir folgende Daten:',
          },
          {
            kind: 'list',
            items: [
              'die Art der Anfrage (Vortrag, Workshop, Interview, Zusammenarbeit oder Sonstiges)',
              'Ihren Namen und Ihre E-Mail-Adresse',
              'optional: Ihre Organisation sowie bei Veranstaltungen Datum, Ort und erwartete Teilnehmerzahl',
              'Ihre Nachricht',
              'den Zeitpunkt des Eingangs der Anfrage, ihren Bearbeitungsstand und die Zeitpunkte, zu denen die E-Mails dazu versandt wurden',
            ],
          },
          {
            kind: 'paragraph',
            text: 'Zweck und Rechtsgrundlage: Wir verwenden diese Daten ausschließlich, um Ihre Anfrage zu beantworten und – sofern sie zu einer Vereinbarung führt, etwa über einen Vortrag – diese vorzubereiten (Art. 6 Abs. 1 lit. b DSGVO). Bei sonstigen Anfragen ist Rechtsgrundlage unser berechtigtes Interesse an der Beantwortung der an uns gerichteten Fragen (Art. 6 Abs. 1 lit. f DSGVO). Sie sind nicht verpflichtet, die Daten anzugeben; ohne sie können wir Ihnen jedoch nicht antworten.',
          },
          {
            kind: 'paragraph',
            text: `Speicherung: Das Formular wird verschlüsselt (HTTPS) an den Server dieser Website übertragen, der die Anfrage in seiner eigenen PostgreSQL-Datenbank speichert. Sie wird ${INQUIRY_RETENTION_DE} nach Eingang automatisch gelöscht (durch einen täglichen Löschlauf, also spätestens einen Tag danach).`,
          },
          {
            kind: 'paragraph',
            text: `Weiterleitung per E-Mail: Nach dem Speichern sendet der Server die Anfrage per E-Mail an uns und Ihnen eine kurze Bestätigung, die keine Ihrer Eingaben wiederholt. Dazu übergibt der Server die E-Mails über eine verschlüsselte Verbindung (TLS) zur Zustellung an folgenden Anbieter: TODO(operator): Name und Anschrift des E-Mail-(SMTP-)Anbieters. Der Anbieter verarbeitet sie ausschließlich in unserem Auftrag (Auftragsverarbeiter, Art. 28 DSGVO). Die Kopie in unserem Postfach bewahren wir bis zur abschließenden Bearbeitung Ihrer Anfrage auf und danach so lange, wie es für die Dokumentation der Geschäftsanbahnung erforderlich ist, längstens ${INQUIRY_MAILBOX_RETENTION_YEARS} Jahre nach dem letzten Kontakt – es sei denn, es kommt ein Vertrag zustande. Vertrags- und Rechnungsdaten bewahren wir gemäß den handels- und steuerrechtlichen Aufbewahrungsfristen (6 bzw. 10 Jahre) auf.`,
          },
          {
            kind: 'paragraph',
            text: 'Schutz vor Missbrauch: Um automatisierten Spam und massenhafte Anfragen zu verhindern, begrenzt der Server die Zahl der Anfragen je IP-Adresse. Dazu wird Ihre IP-Adresse (bei IPv6 deren Netzanteil) ausschließlich im Arbeitsspeicher des Servers gehalten, nie auf einen Datenträger geschrieben und entfernt, sobald Ihr Kontingent wieder aufgefüllt ist – bei den Standardeinstellungen spätestens 70 Minuten nach Ihrer letzten Anfrage. Zusammen mit der Anfrage selbst speichern wir Ihre IP-Adresse nur als gesalzenen Hashwert: einen Wert, aus dem sich die Adresse ohne unseren geheimen Schlüssel nicht zurückgewinnen lässt, der es uns aber ermöglicht, mehrere Anfragen von derselben Adresse zu erkennen. Der Hashwert ist weiterhin ein personenbezogenes Datum (pseudonymisiert) und wird zusammen mit der Anfrage gelöscht. Rechtsgrundlage ist unser berechtigtes Interesse am Schutz des Formulars vor Missbrauch (Art. 6 Abs. 1 lit. f DSGVO).',
          },
          {
            kind: 'paragraph',
            text: 'Wir geben Ihre Anfrage nicht an Dritte weiter und verwenden sie weder für Werbung noch für Newsletter.',
          },
        ],
      },
      {
        id: 'external-links',
        heading: 'Externe Links',
        blocks: [
          {
            kind: 'paragraph',
            text: 'Diese Website verlinkt auf andere Websites, vor allem auf GitHub und LinkedIn. Erst wenn Sie einem solchen Link folgen, nimmt Ihr Browser Verbindung zu der jeweiligen Website auf; dort gilt dann deren eigene Datenschutzerklärung.',
          },
        ],
      },
      {
        id: 'third-countries',
        heading: 'Übermittlung in Drittländer',
        blocks: [
          {
            kind: 'paragraph',
            text: 'Wir übermitteln auf dieser Website erhobene personenbezogene Daten nicht in Länder außerhalb der Europäischen Union oder des Europäischen Wirtschaftsraums.',
          },
        ],
      },
      {
        id: 'rights',
        heading: 'Ihre Rechte',
        blocks: [
          {
            kind: 'paragraph',
            text: 'Sie haben das Recht auf Auskunft über die bei uns zu Ihrer Person gespeicherten Daten (Art. 15 DSGVO), auf deren Berichtigung (Art. 16 DSGVO) oder Löschung (Art. 17 DSGVO), auf Einschränkung ihrer Verarbeitung (Art. 18 DSGVO) sowie darauf, sie in einem übertragbaren Format zu erhalten (Datenübertragbarkeit, Art. 20 DSGVO).',
          },
          {
            kind: 'paragraph',
            text: 'Widerspruchsrecht: Soweit wir Daten auf Grundlage unseres berechtigten Interesses verarbeiten (Art. 6 Abs. 1 lit. f DSGVO), können Sie aus Gründen, die sich aus Ihrer besonderen Situation ergeben, jederzeit Widerspruch gegen diese Verarbeitung einlegen (Art. 21 DSGVO).',
          },
          {
            kind: 'paragraph',
            text: 'Soweit wir Daten auf Grundlage Ihrer Einwilligung verarbeiten, können Sie diese jederzeit mit Wirkung für die Zukunft widerrufen (Art. 7 Abs. 3 DSGVO); die Rechtmäßigkeit der bis zum Widerruf erfolgten Verarbeitung bleibt davon unberührt.',
          },
          {
            kind: 'paragraph',
            text: 'Außerdem haben Sie das Recht, sich bei einer Datenschutz-Aufsichtsbehörde zu beschweren (Art. 77 DSGVO). Die für uns zuständige Aufsichtsbehörde ist die Landesbeauftragte für Datenschutz und Informationsfreiheit Nordrhein-Westfalen.',
          },
        ],
      },
      {
        id: 'privacy-contact',
        heading: 'Kontakt für Datenschutzanfragen',
        blocks: [
          {
            kind: 'paragraph',
            text: 'Wenn Sie Ihre Rechte geltend machen möchten oder Fragen zu dieser Datenschutzerklärung haben, schreiben Sie uns bitte an ',
            link: { label: OPERATOR_EMAIL, href: `mailto:${OPERATOR_EMAIL}` },
            after:
              '. Sie können uns jederzeit bitten, eine von Ihnen gesendete Anfrage zu löschen.',
          },
        ],
      },
      {
        id: 'changes',
        heading: 'Änderungen dieser Datenschutzerklärung',
        blocks: [
          {
            kind: 'paragraph',
            text: 'Wir passen diese Datenschutzerklärung an, wenn sich die Website oder die Rechtslage ändert. Es gilt die jeweils hier veröffentlichte Fassung.',
          },
        ],
      },
    ],
  },
};
