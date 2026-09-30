package org.obie.website;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.within;

import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.stream.Stream;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.TestInstance;
import org.junit.jupiter.api.TestInstance.Lifecycle;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.Arguments;
import org.junit.jupiter.params.provider.MethodSource;
import org.openqa.selenium.JavascriptExecutor;
import org.openqa.selenium.WebDriver;
import org.openqa.selenium.chrome.ChromeOptions;
import org.openqa.selenium.remote.RemoteWebDriver;
import org.openqa.selenium.support.ui.WebDriverWait;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.testcontainers.Testcontainers;
import org.testcontainers.containers.BrowserWebDriverContainer;
import org.testcontainers.containers.BrowserWebDriverContainer.VncRecordingMode;
import org.testcontainers.utility.DockerImageName;

/**
 * The site header in a real browser: Chromium (in a container) opens the packaged, prerendered home
 * page in English and German at phone, tablet and desktop widths in both themes. The navigation
 * lists only the page sections, its links share one baseline per row and one gap throughout, the
 * actions (language switch, theme toggle, "View on GitHub") stay beside the brand without the page
 * scrolling sideways, and on wide screens brand, links and actions form a single row of the design
 * height, the links on the baseline of "View on GitHub".
 */
@TestInstance(Lifecycle.PER_CLASS)
class SiteHeaderBrowserTest extends IntegrationTest {

  /** Matches the Selenium client version managed by Spring Boot. */
  private static final DockerImageName CHROMIUM =
      DockerImageName.parse("selenium/standalone-chromium:4.31.0")
          .asCompatibleSubstituteFor("selenium/standalone-chrome");

  private static final Duration TIMEOUT = Duration.ofSeconds(30);

  /** The section links per language, from the front end's content files, in page order. */
  private static final Map<String, List<String>> SECTIONS =
      Map.of(
          "en",
          List.of("The problem", "How it works", "Principles", "Status", "Get started", "FAQ"),
          "de",
          List.of(
              "Problem", "So funktioniert es", "Prinzipien", "Status", "Erste Schritte", "FAQ"));

  /** The invitation to speak per language, which belongs to the footer, not the header. */
  private static final Map<String, String> INVITATION =
      Map.of("en", "Invite Markus to speak", "de", "Markus als Redner einladen");

  /** The home page per language. */
  private static final Map<String, String> HOME = Map.of("en", "/", "de", "/de");

  /** Subpixel rounding between boxes that line up. */
  private static final double TOLERANCE = 0.5;

  /**
   * Per section link its label, text baseline and edges; the baseline of the GitHub label; the
   * height of the header bar and its minimum height. The bottom of a text box stands for its
   * baseline: all these labels share one font and size.
   */
  private static final String MEASURE =
      """
      const header = document.querySelector('app-site-header header');
      const textBottom = (node) => {
        const range = document.createRange();
        range.selectNodeContents(node);
        return range.getBoundingClientRect().bottom;
      };
      const github = [...header.querySelector('a.github').childNodes]
        .find((node) => node.nodeType === Node.TEXT_NODE && node.textContent.trim());
      const bar = header.querySelector('.bar');
      const box = (selector) => header.querySelector(selector).getBoundingClientRect();
      return {
        text: header.textContent,
        brandBottom: box('.brand').bottom,
        actionsTop: box('.actions').top,
        actionsRight: box('.actions').right,
        viewportWidth: document.documentElement.clientWidth,
        scrollWidth: document.documentElement.scrollWidth,
        links: [...header.querySelectorAll('nav a')].map((a) => {
          const box = a.getBoundingClientRect();
          return {label: a.textContent.trim(), baseline: textBottom(a), left: box.left, right: box.right};
        }),
        githubBaseline: textBottom(github),
        barHeight: bar.getBoundingClientRect().height,
        barMinHeight: parseFloat(getComputedStyle(bar).minHeight) || 0,
      };
      """;

  @LocalServerPort private int port;

  private BrowserWebDriverContainer<?> browser;
  private WebDriver driver;

  @BeforeAll
  void startBrowser() {
    Testcontainers.exposeHostPorts(port);
    browser =
        new BrowserWebDriverContainer<>(CHROMIUM)
            .withCapabilities(new ChromeOptions())
            .withRecordingMode(VncRecordingMode.SKIP, null);
    browser.start();
  }

  @AfterAll
  void stopBrowser() {
    browser.stop();
  }

  @AfterEach
  void closeBrowser() {
    if (driver != null) {
      driver.quit();
      driver = null;
    }
  }

  static Stream<Arguments> everyWidth() {
    return combinations(List.of(360, 768, 1024, 1440));
  }

  static Stream<Arguments> wideScreens() {
    return combinations(List.of(1024, 1440));
  }

  private static Stream<Arguments> combinations(List<Integer> widths) {
    return Stream.of("en", "de")
        .flatMap(
            lang ->
                widths.stream()
                    .flatMap(
                        width ->
                            Stream.of("light", "dark")
                                .map(theme -> Arguments.of(width, theme, lang))));
  }

  @ParameterizedTest(name = "{0} px, {1} theme, {2}")
  @MethodSource("everyWidth")
  void theNavigationListsOnlyTheSectionsOnOneBaselinePerRowWithOneGap(
      int width, String theme, String lang) {
    Map<String, Object> header = open(width, theme, lang);
    List<Link> links = links(header);

    assertThat(links).extracting(Link::label).containsExactlyElementsOf(SECTIONS.get(lang));
    assertThat((String) header.get("text")).doesNotContain(INVITATION.get(lang));
    // The actions fit beside the brand, and nothing pushes the page sideways.
    assertThat(number(header.get("actionsTop"))).isLessThan(number(header.get("brandBottom")));
    assertThat(number(header.get("actionsRight")))
        .isLessThanOrEqualTo(number(header.get("viewportWidth")) + TOLERANCE);
    assertThat(number(header.get("scrollWidth")))
        .isLessThanOrEqualTo(number(header.get("viewportWidth")));
    List<Double> gaps = new ArrayList<>();
    for (List<Link> row : rows(links)) {
      double baseline = row.get(0).baseline();
      assertThat(row)
          .allSatisfy(link -> assertThat(link.baseline()).isCloseTo(baseline, within(TOLERANCE)));
      for (int i = 1; i < row.size(); i++) {
        gaps.add(row.get(i).left() - row.get(i - 1).right());
      }
    }
    // Links stacked one per row would leave nothing to compare.
    assertThat(gaps).isNotEmpty();
    assertThat(gaps).allSatisfy(gap -> assertThat(gap).isCloseTo(gaps.get(0), within(TOLERANCE)));
  }

  @ParameterizedTest(name = "{0} px, {1} theme, {2}")
  @MethodSource("wideScreens")
  void onWideScreensTheHeaderIsOneRowOfTheDesignHeight(int width, String theme, String lang) {
    Map<String, Object> header = open(width, theme, lang);
    List<Link> links = links(header);

    assertThat(rows(links)).hasSize(1);
    double github = number(header.get("githubBaseline"));
    assertThat(links)
        .allSatisfy(link -> assertThat(link.baseline()).isCloseTo(github, within(TOLERANCE)));
    // A second row, such as a wrapped call to action, would make the bar taller.
    assertThat(number(header.get("barHeight")))
        .isCloseTo(number(header.get("barMinHeight")), within(TOLERANCE));
  }

  /**
   * Opens the home page in {@code lang}, {@code width} CSS pixels wide in {@code theme}, and
   * measures the header once the web fonts are in.
   */
  private Map<String, Object> open(int width, String theme, String lang) {
    ChromeOptions options = new ChromeOptions();
    options.setExperimentalOption(
        "mobileEmulation",
        Map.of("deviceMetrics", Map.of("width", width, "height", 900, "pixelRatio", 1)));
    driver = new RemoteWebDriver(browser.getSeleniumAddress(), options);
    driver.get("http://host.testcontainers.internal:" + port + HOME.get(lang));
    // What the header's theme toggle does; nothing else writes data-theme.
    script("document.documentElement.setAttribute('data-theme', arguments[0])", theme);
    assertThat(script("return getComputedStyle(document.documentElement).colorScheme"))
        .isEqualTo(theme);
    new WebDriverWait(driver, TIMEOUT)
        .until(d -> (Boolean) script("return document.fonts.status === 'loaded'"));
    @SuppressWarnings("unchecked")
    Map<String, Object> header = (Map<String, Object>) script(MEASURE);
    return header;
  }

  private static List<Link> links(Map<String, Object> header) {
    @SuppressWarnings("unchecked")
    List<Map<String, Object>> links = (List<Map<String, Object>>) header.get("links");
    return links.stream()
        .map(
            link ->
                new Link(
                    (String) link.get("label"),
                    number(link.get("baseline")),
                    number(link.get("left")),
                    number(link.get("right"))))
        .toList();
  }

  /** The links grouped into the rows they wrap into: a row ends where the next link starts left. */
  private static List<List<Link>> rows(List<Link> links) {
    List<List<Link>> rows = new ArrayList<>();
    for (Link link : links) {
      if (rows.isEmpty() || link.left() < rows.getLast().getLast().right()) {
        rows.add(new ArrayList<>());
      }
      rows.getLast().add(link);
    }
    return rows;
  }

  private static double number(Object value) {
    return ((Number) value).doubleValue();
  }

  private Object script(String code, Object... arguments) {
    return ((JavascriptExecutor) driver).executeScript(code, arguments);
  }

  private record Link(String label, double baseline, double left, double right) {}
}
