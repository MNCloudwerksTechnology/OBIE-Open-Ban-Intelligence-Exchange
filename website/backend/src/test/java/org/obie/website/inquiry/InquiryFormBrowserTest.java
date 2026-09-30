package org.obie.website.inquiry;

import static org.assertj.core.api.Assertions.assertThat;
import static org.awaitility.Awaitility.await;

import jakarta.mail.MessagingException;
import jakarta.mail.internet.MimeMessage;
import java.time.Duration;
import java.util.Arrays;
import java.util.UUID;
import org.junit.jupiter.api.AfterAll;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.TestInstance;
import org.junit.jupiter.api.TestInstance.Lifecycle;
import org.obie.website.IntegrationTest;
import org.openqa.selenium.By;
import org.openqa.selenium.JavascriptExecutor;
import org.openqa.selenium.WebDriver;
import org.openqa.selenium.chrome.ChromeOptions;
import org.openqa.selenium.remote.RemoteWebDriver;
import org.openqa.selenium.support.ui.ExpectedConditions;
import org.openqa.selenium.support.ui.WebDriverWait;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.testcontainers.Testcontainers;
import org.testcontainers.containers.BrowserWebDriverContainer;
import org.testcontainers.containers.BrowserWebDriverContainer.VncRecordingMode;
import org.testcontainers.utility.DockerImageName;

/**
 * End to end in a real browser: Chromium (in a container) opens the packaged, prerendered site
 * served by the running application, fills in the inquiry form and sends it. The inquiry must be
 * stored in PostgreSQL and mailed to the operator. This also proves that the page hydrates under
 * the production Content Security Policy and that the form waits out the minimum fill time.
 */
@TestInstance(Lifecycle.PER_CLASS)
class InquiryFormBrowserTest extends IntegrationTest {

  /** Matches the Selenium client version managed by Spring Boot. */
  private static final DockerImageName CHROMIUM =
      DockerImageName.parse("selenium/standalone-chromium:4.31.0")
          .asCompatibleSubstituteFor("selenium/standalone-chrome");

  private static final Duration TIMEOUT = Duration.ofSeconds(30);

  @LocalServerPort private int port;
  @Autowired private InquiryRepository repository;
  @Autowired private InquiryProperties properties;

  private BrowserWebDriverContainer<?> browser;
  private WebDriver driver;

  @BeforeAll
  void startBrowser() {
    // The browser reaches the application on the host through this name; the
    // port must be exposed before the container starts.
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

  @BeforeEach
  void openBrowser() {
    ChromeOptions options = new ChromeOptions();
    // Without smooth scrolling, an element is where Selenium expects it when it clicks.
    options.addArguments("--force-prefers-reduced-motion");
    driver = new RemoteWebDriver(browser.getSeleniumAddress(), options);
  }

  @AfterEach
  void closeBrowser() {
    driver.quit();
  }

  @Test
  void visitorSendsAnInquiryThatIsStoredAndMailed() {
    String name = "Grace Hopper " + UUID.randomUUID();
    String message = "Would you give a talk about OBIE at our conference in spring?";

    driver.get("http://host.testcontainers.internal:" + port + "/#contact");
    WebDriverWait wait = new WebDriverWait(driver, TIMEOUT);
    // The form fetches its token once the page is hydrated; typing earlier
    // would go into the prerendered markup only.
    wait.until(
        d ->
            (Boolean)
                ((JavascriptExecutor) d)
                    .executeScript(
                        "return performance.getEntriesByType('resource')"
                            + ".some(e => e.name.includes('/api/inquiries/form-token'))"));
    // A first visit is asked about visitor statistics; the dialog covers the bottom of the window.
    wait.until(
            ExpectedConditions.elementToBeClickable(
                By.xpath("//app-consent-dialog//button[normalize-space()='Decline']")))
        .click();
    wait.until(
        ExpectedConditions.invisibilityOfElementLocated(By.cssSelector("app-consent-dialog *")));

    driver.findElement(By.xpath("//label[normalize-space()='Talk']/input")).click();
    type("inquiry-name", name);
    type("inquiry-email", "grace@example.org");
    type("inquiry-organisation", "Compiler Conference");
    type("inquiry-eventLocation", "online");
    type("inquiry-audienceSize", "250");
    type("inquiry-message", message);
    driver.findElement(By.id("inquiry-consent")).click();
    driver.findElement(By.cssSelector("#contact button[type=submit]")).click();

    wait.until(
        ExpectedConditions.textToBePresentInElementLocated(
            By.cssSelector("#contact .panel--success"),
            "Thanks, Markus will get back to you within a few days."));

    Inquiry stored =
        await()
            .atMost(TIMEOUT)
            .until(
                () ->
                    repository.findAll().stream()
                        .filter(inquiry -> name.equals(inquiry.getName()))
                        .findFirst()
                        .orElse(null),
                inquiry -> inquiry != null);
    assertThat(stored.getType()).isEqualTo(InquiryType.TALK);
    assertThat(stored.getEmail()).isEqualTo("grace@example.org");
    assertThat(stored.getOrganisation()).isEqualTo("Compiler Conference");
    assertThat(stored.getEventLocation()).isEqualTo("online");
    assertThat(stored.getAudienceSize()).isEqualTo(250);
    assertThat(stored.getEventDate()).isNull();
    assertThat(stored.getMessage()).isEqualTo(message);

    String subject = "[OBIE inquiry] talk from " + name;
    await()
        .atMost(TIMEOUT)
        .until(
            () ->
                Arrays.stream(SMTP.getReceivedMessages())
                    .anyMatch(mail -> isNotification(mail, subject)));
  }

  /**
   * The form's code loads lazily (ADR 0015), but soon after the page has loaded, not only when the
   * visitor scrolls to it: submitted before hydration, the prerendered form would reload the page.
   */
  @Test
  void formIsReadyBeforeTheVisitorScrollsToIt() {
    String home = "http://host.testcontainers.internal:" + port + "/";
    driver.get(home);
    JavascriptExecutor js = (JavascriptExecutor) driver;
    new WebDriverWait(driver, TIMEOUT)
        .until(
            d ->
                (Boolean)
                    js.executeScript(
                        "return performance.getEntriesByType('resource')"
                            + ".some(e => e.name.includes('/api/inquiries/form-token'))"));
    assertThat((Boolean) js.executeScript("return scrollY === 0")).isTrue();

    js.executeScript("document.querySelector('#contact form').requestSubmit()");

    // Hydrated, the form validates in place and flags the empty required fields.
    new WebDriverWait(driver, TIMEOUT)
        .until(
            d ->
                Boolean.TRUE.equals(
                    js.executeScript(
                        "return document.querySelectorAll('#contact [aria-invalid=\"true\"]')"
                            + ".length > 0")));
    assertThat(driver.getCurrentUrl()).isEqualTo(home);
  }

  private void type(String id, String text) {
    driver.findElement(By.id(id)).sendKeys(text);
  }

  /** Whether the mail is the operator's notification with this subject. */
  private boolean isNotification(MimeMessage mail, String subject) {
    try {
      return subject.equals(mail.getSubject())
          && Arrays.stream(mail.getAllRecipients())
              .anyMatch(address -> address.toString().equals(properties.recipient()));
    } catch (MessagingException e) {
      return false;
    }
  }
}
