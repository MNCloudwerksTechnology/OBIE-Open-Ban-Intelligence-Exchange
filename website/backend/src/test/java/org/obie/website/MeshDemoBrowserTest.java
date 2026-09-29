package org.obie.website;

import static org.assertj.core.api.Assertions.assertThat;

import java.time.Duration;
import java.util.List;
import java.util.Map;
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
 * JavaScript it hydrates and the visitor steps through it; with reduced motion no animation runs.
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
      driver.quit();
      driver = null;
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

  private void open(ChromeOptions options) {
    driver = new RemoteWebDriver(browser.getSeleniumAddress(), options);
    driver.get("http://host.testcontainers.internal:" + port + "/");
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
