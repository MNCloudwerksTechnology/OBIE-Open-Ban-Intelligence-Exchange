package org.obie.website;

import static org.assertj.core.api.Assertions.assertThat;

import java.time.Duration;
import java.util.List;
import java.util.Map;
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
 * The three-node demo of "How it works" (ADR 0028) in a real browser: Chromium (in a container)
 * opens the packaged, prerendered home page served by the running application under the production
 * Content Security Policy. Without JavaScript the demo is the ordered list of its ten steps; with
 * JavaScript it hydrates without errors and the visitor steps through it; with reduced motion no
 * animation runs; a phone never scrolls sideways; a link to a section below the demo stays on its
 * target when the demo hydrates.
 */
@TestInstance(Lifecycle.PER_CLASS)
class MeshDemoBrowserTest extends IntegrationTest {

  /** Matches the Selenium client version managed by Spring Boot. */
  private static final DockerImageName CHROMIUM =
      DockerImageName.parse("selenium/standalone-chromium:4.31.0")
          .asCompatibleSubstituteFor("selenium/standalone-chrome");

  private static final Duration TIMEOUT = Duration.ofSeconds(30);

  /** The climax of the story, step 6, from the front end's content file. */
  private static final String STEP_SIX = "C is protected before the attack arrives";

  /** Every running or filling animation of the demo, its map included. */
  private static final String ANIMATIONS =
      "return document.querySelector('app-mesh-demo').getAnimations({subtree: true}).length";

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
      List<String> errors =
          driver.manage().logs().get(LogType.BROWSER).getAll().stream()
              .filter(entry -> entry.getLevel().intValue() >= Level.SEVERE.intValue())
              .map(LogEntry::getMessage)
              // The test serves plain HTTP on a host name other than localhost, so Chrome
              // ignores the Cross-Origin-Opener-Policy header; production serves HTTPS.
              .filter(
                  message ->
                      !message.contains("Cross-Origin-Opener-Policy header has been ignored"))
              .toList();
      driver.quit();
      driver = null;
      // A hydration mismatch, a CSP violation or a failed request would show up here.
      assertThat(errors).isEmpty();
    }
  }

  @Test
  void withoutJavaScriptTheDemoIsTheOrderedListOfItsTenSteps() {
    ChromeOptions options = new ChromeOptions();
    options.setExperimentalOption(
        "prefs", Map.of("profile.managed_default_content_settings.javascript", 2));
    open(options);

    List<WebElement> steps = driver.findElements(By.cssSelector("app-mesh-demo > ol.story > li"));
    assertThat(steps).hasSize(10);
    assertThat(steps.get(5).findElement(By.tagName("strong")).getText()).isEqualTo(STEP_SIX);
    assertThat(driver.findElements(By.cssSelector("app-mesh-demo button"))).isEmpty();
    // Nothing hydrated: the demo never ran.
    assertThat(driver.findElements(By.cssSelector("app-mesh-demo[data-motion]"))).isEmpty();
  }

  @Test
  void theDemoHydratesUnderTheContentSecurityPolicyAndTurnsTheBotAwayAtStepSix() {
    open(new ChromeOptions());

    goToStep(6);

    assertThat(driver.findElement(By.cssSelector("app-mesh-demo .narrative .title")).getText())
        .isEqualTo(STEP_SIX);
    WebElement bot = driver.findElement(By.cssSelector(".servers > li:nth-child(3) .row"));
    assertThat(bot.getDomAttribute("data-state")).isEqualTo("blocked");
    assertThat(bot.getText()).contains("Score 1.28 of 1.20 needed", "2 of 2 reporters");
    assertThat(driver.findElements(By.cssSelector("app-mesh-demo .attack--turned-away")))
        .hasSize(1);
    assertThat(demo().getDomAttribute("data-motion")).isEqualTo("full");
    assertThat((Long) script(ANIMATIONS)).isPositive();
  }

  @Test
  void withReducedMotionTheStepsChangeWithoutAnimation() {
    ChromeOptions options = new ChromeOptions();
    options.addArguments("--force-prefers-reduced-motion");
    open(options);

    goToStep(3);

    assertThat(demo().getDomAttribute("data-motion")).isEqualTo("reduced");
    assertThat((Long) script(ANIMATIONS)).isZero();
    assertThat(driver.findElements(By.cssSelector("app-mesh-demo .message"))).hasSize(4);
  }

  @Test
  void onAPhoneNoStepScrollsSidewaysAndEverythingComesFromTheSite() {
    ChromeOptions options = new ChromeOptions();
    options.setExperimentalOption(
        "mobileEmulation",
        Map.of("deviceMetrics", Map.of("width", 360, "height", 780, "pixelRatio", 2)));
    options.addArguments("--force-prefers-reduced-motion");
    open(options);

    for (int step = 1; step <= 10; step++) {
      goToStep(step);
      assertThat(
              (Boolean)
                  script(
                      "return document.documentElement.scrollWidth"
                          + " <= document.documentElement.clientWidth"))
          .as("step %d scrolls sideways", step)
          .isTrue();
    }
    assertThat(
            (Boolean)
                script(
                    "return performance.getEntriesByType('resource')"
                        + ".every(e => e.name.startsWith(location.origin))"))
        .isTrue();
  }

  @Test
  void aLinkToASectionBelowTheDemoStaysOnItsTargetWhenTheDemoHydrates() {
    ChromeOptions options = new ChromeOptions();
    options.addArguments("--window-size=1440,900", "--force-prefers-reduced-motion");
    open(options, "/#get-started");
    WebDriverWait wait = new WebDriverWait(driver, TIMEOUT);
    wait.until(
        ExpectedConditions.presenceOfElementLocated(By.cssSelector("app-mesh-demo .toolbar")));

    // The section still starts just below the sticky header, at scroll-padding-top.
    Number offset =
        (Number)
            script(
                "return document.querySelector('#get-started').getBoundingClientRect().top"
                    + " - parseFloat(getComputedStyle(document.documentElement).scrollPaddingTop)");
    assertThat(offset.doubleValue()).isBetween(-2.0, 2.0);
  }

  private void open(ChromeOptions options) {
    open(options, "/");
  }

  private void open(ChromeOptions options, String path) {
    options.setCapability("goog:loggingPrefs", Map.of(LogType.BROWSER, "ALL"));
    driver = new RemoteWebDriver(browser.getSeleniumAddress(), options);
    driver.get("http://host.testcontainers.internal:" + port + path);
  }

  /** Waits for the demo to hydrate (on idle), then jumps to step {@code n} with its step button. */
  private void goToStep(int n) {
    WebDriverWait wait = new WebDriverWait(driver, TIMEOUT);
    WebElement button =
        wait.until(
            ExpectedConditions.presenceOfElementLocated(
                By.cssSelector("app-mesh-demo .steps li:nth-child(" + n + ") button")));
    // A script click needs no scrolling, which would be smooth without reduced motion.
    script("arguments[0].click()", button);
    wait.until(
        ExpectedConditions.attributeToBe(
            By.cssSelector("app-mesh-demo .steps li:nth-child(" + n + ") button"),
            "aria-current",
            "step"));
  }

  private WebElement demo() {
    return driver.findElement(By.tagName("app-mesh-demo"));
  }

  private Object script(String code, Object... arguments) {
    return ((JavascriptExecutor) driver).executeScript(code, arguments);
  }
}
