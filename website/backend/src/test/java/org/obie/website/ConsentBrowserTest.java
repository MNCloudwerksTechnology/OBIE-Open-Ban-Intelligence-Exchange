package org.obie.website;

import static org.assertj.core.api.Assertions.assertThat;

import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.function.Predicate;
import java.util.logging.Level;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.TestInstance;
import org.junit.jupiter.api.TestInstance.Lifecycle;
import org.openqa.selenium.By;
import org.openqa.selenium.JavascriptExecutor;
import org.openqa.selenium.WebDriver;
import org.openqa.selenium.WebElement;
import org.openqa.selenium.chrome.ChromeOptions;
import org.openqa.selenium.logging.LogEntry;
import org.openqa.selenium.logging.LogType;
import org.openqa.selenium.remote.RemoteWebDriver;
import org.openqa.selenium.support.ui.ExpectedConditions;
import org.openqa.selenium.support.ui.WebDriverWait;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.testcontainers.Testcontainers;
import org.testcontainers.containers.BrowserWebDriverContainer;
import org.testcontainers.containers.BrowserWebDriverContainer.VncRecordingMode;
import org.testcontainers.utility.DockerImageName;

/**
 * The question about visitor statistics and the German pages in a real browser (ADR 0032, ADR
 * 0033): Chromium (in a container) opens the packaged, prerendered pages under the production
 * Content Security Policy. Before the visitor accepts, the browser contacts no other origin;
 * declining is remembered; accepting loads Matomo from the one origin the policy allows. German
 * pages hydrate in German without errors, and the language switch leads to the same page in
 * English.
 *
 * <p>The browser resolves the statistics server to nowhere, so no test ever reaches the real one.
 */
@TestInstance(Lifecycle.PER_CLASS)
class ConsentBrowserTest extends IntegrationTest {

  /** Matches the Selenium client version managed by Spring Boot. */
  private static final DockerImageName CHROMIUM =
      DockerImageName.parse("selenium/standalone-chromium:4.31.0")
          .asCompatibleSubstituteFor("selenium/standalone-chrome");

  private static final Duration TIMEOUT = Duration.ofSeconds(30);

  /** Host of the self-hosted Matomo (front end: analytics.config.ts). */
  private static final String MATOMO_HOST = "metrics.cloudwerks.de";

  private static final String DIALOG = "app-consent-dialog [role='dialog']";

  /** Whether every resource the page loaded came from the site itself. */
  private static final String ONLY_OWN_ORIGIN =
      "return performance.getEntriesByType('resource')"
          + ".every(e => e.name.startsWith(location.origin))";

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

  @Test
  void theFirstVisitAsksAndContactsNoOtherOriginUntilTheVisitorAccepts() {
    open("/");

    WebElement dialog = waitFor(DIALOG);
    assertThat(dialog.getText()).contains("Visitor statistics", MATOMO_HOST);
    assertThat(dialog.findElements(By.tagName("button")))
        .extracting(WebElement::getText)
        .containsExactly("Decline", "Accept");
    assertThat((Boolean) script(ONLY_OWN_ORIGIN)).isTrue();
    assertThat(script("return window._paq === undefined")).isEqualTo(true);
    assertThat(severeErrors()).isEmpty();
  }

  @Test
  void aDeclineIsRememberedAndNothingIsLoaded() {
    open("/");
    click(waitFor(DIALOG).findElement(By.xpath(".//button[normalize-space()='Decline']")));
    new WebDriverWait(driver, TIMEOUT)
        .until(ExpectedConditions.invisibilityOfElementLocated(By.cssSelector(DIALOG)));

    driver.navigate().refresh();
    waitFor("app-site-footer nav button");

    assertThat(driver.findElements(By.cssSelector(DIALOG))).isEmpty();
    assertThat(script("return localStorage.getItem('obie-analytics-consent-v1')"))
        .isEqualTo("denied");
    assertThat((Boolean) script(ONLY_OWN_ORIGIN)).isTrue();
    assertThat(severeErrors()).isEmpty();
  }

  @Test
  void acceptingLoadsMatomoFromTheOneOriginThePolicyAllows() {
    open("/");
    script(
        "window.__cspViolations = [];"
            + "document.addEventListener('securitypolicyviolation',"
            + " e => window.__cspViolations.push(e.blockedURI + ' ' + e.violatedDirective));");

    click(waitFor(DIALOG).findElement(By.xpath(".//button[normalize-space()='Accept']")));
    new WebDriverWait(driver, TIMEOUT)
        .until(d -> script("return document.querySelector(\"script[src*='matomo.js']\")") != null);

    assertThat(script("return document.querySelector(\"script[src*='matomo.js']\").src"))
        .isEqualTo("https://" + MATOMO_HOST + "/matomo.js");
    @SuppressWarnings("unchecked")
    List<List<Object>> queue = (List<List<Object>>) script("return window._paq");
    assertThat(queue)
        .extracting(command -> command.get(0))
        .contains("requireConsent", "disableCookies", "setSiteId", "trackPageView");
    assertThat(script("return window.__cspViolations")).asList().isEmpty();
    assertThat(script("return document.cookie")).isEqualTo("");
    // The script cannot load here (see the class comment); a CSP violation would be a different
    // error.
    assertThat(severeErrors(message -> !message.contains(MATOMO_HOST))).isEmpty();
  }

  @Test
  void thePrivacySettingsReopenTheQuestionAndTakeFocus() {
    open("/");
    click(waitFor(DIALOG).findElement(By.xpath(".//button[normalize-space()='Decline']")));

    click(waitFor("app-site-footer nav button"));

    WebElement dialog = waitFor(DIALOG);
    assertThat(dialog.getText()).contains("You currently decline visitor statistics.");
    assertThat(script("return document.activeElement === arguments[0]", dialog)).isEqualTo(true);
  }

  @Test
  void germanPagesHydrateInGermanAndSwitchToTheSamePageInEnglish() {
    open("/de/datenschutz");

    WebElement dialog = waitFor(DIALOG);
    assertThat(dialog.getText()).contains("Besucherstatistik");
    assertThat(script("return document.documentElement.lang")).isEqualTo("de");
    assertThat(driver.findElement(By.cssSelector("main h1")).getText())
        .isEqualTo("Datenschutzerklärung");
    assertThat(severeErrors()).isEmpty();

    click(driver.findElement(By.cssSelector("app-site-header app-language-switch a")));
    new WebDriverWait(driver, TIMEOUT).until(ExpectedConditions.urlMatches("/privacy$"));
    waitFor(DIALOG);

    assertThat(script("return document.documentElement.lang")).isEqualTo("en");
    assertThat(driver.findElement(By.cssSelector("main h1")).getText()).contains("Privacy policy");
    assertThat(severeErrors()).isEmpty();
  }

  private void open(String path) {
    ChromeOptions options = new ChromeOptions();
    options.setCapability("goog:loggingPrefs", Map.of(LogType.BROWSER, "ALL"));
    options.addArguments(
        "--force-prefers-reduced-motion",
        "--host-resolver-rules=MAP " + MATOMO_HOST + " ~NOTFOUND");
    driver = new RemoteWebDriver(browser.getSeleniumAddress(), options);
    driver.get("http://host.testcontainers.internal:" + port + path);
  }

  /** Waits until the element is shown: the dialog appears once the page has hydrated. */
  private WebElement waitFor(String selector) {
    return new WebDriverWait(driver, TIMEOUT)
        .until(ExpectedConditions.visibilityOfElementLocated(By.cssSelector(selector)));
  }

  private void click(WebElement element) {
    script("arguments[0].click()", element);
  }

  /** Browser errors since the last call, e.g. a hydration mismatch or a CSP violation. */
  private List<String> severeErrors() {
    return severeErrors(message -> true);
  }

  private List<String> severeErrors(Predicate<String> relevant) {
    List<LogEntry> entries = driver.manage().logs().get(LogType.BROWSER).getAll();
    return entries.stream()
        .filter(entry -> entry.getLevel().intValue() >= Level.SEVERE.intValue())
        .map(LogEntry::getMessage)
        // Plain HTTP on a host name other than localhost; production serves HTTPS.
        .filter(message -> !message.contains("Cross-Origin-Opener-Policy header has been ignored"))
        .filter(relevant)
        .toList();
  }

  private Object script(String code, Object... arguments) {
    return ((JavascriptExecutor) driver).executeScript(code, arguments);
  }
}
