package org.obie.website;

import com.icegreen.greenmail.util.GreenMail;
import com.icegreen.greenmail.util.ServerSetupTest;
import java.io.IOException;
import java.io.UncheckedIOException;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.context.SpringBootTest.WebEnvironment;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.context.DynamicPropertyRegistry;
import org.springframework.test.context.DynamicPropertySource;
import org.testcontainers.containers.Container.ExecResult;
import org.testcontainers.containers.PostgreSQLContainer;

/**
 * Base of every test that boots the application: a real PostgreSQL in a container (Testcontainers)
 * and an in-process SMTP server (GreenMail), both started once per test run and shared, plus the
 * {@code test} profile ({@code application-test.properties}).
 */
@SpringBootTest(webEnvironment = WebEnvironment.RANDOM_PORT)
@ActiveProfiles("test")
public abstract class IntegrationTest {

  protected static final PostgreSQLContainer<?> POSTGRES =
      new PostgreSQLContainer<>("postgres:16-alpine");

  protected static final GreenMail SMTP = new GreenMail(ServerSetupTest.SMTP.dynamicPort());

  static {
    // Stopped by Testcontainers' reaper and at JVM exit respectively.
    POSTGRES.start();
    SMTP.start();
  }

  /**
   * JDBC URL of a separate database in the shared container, created on first use. Every cached
   * application context runs its own mail dispatcher; a test whose context must not see (or be seen
   * by) the others' inquiries uses its own database.
   */
  protected static String separateDatabase(String name) {
    try {
      ExecResult result = POSTGRES.execInContainer("createdb", "-U", POSTGRES.getUsername(), name);
      if (result.getExitCode() != 0 && !result.getStderr().contains("already exists")) {
        throw new IllegalStateException("Cannot create database " + name + ": " + result);
      }
    } catch (IOException e) {
      throw new UncheckedIOException(e);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw new IllegalStateException(e);
    }
    return POSTGRES.getJdbcUrl().replaceFirst("/[^/?]+(\\?|$)", "/" + name + "$1");
  }

  /**
   * Registered under {@code test.*} and mapped to the Spring keys in {@code
   * application-test.properties}, so a subclass can still override the Spring keys with its own
   * {@code @DynamicPropertySource}.
   */
  @DynamicPropertySource
  static void infrastructure(DynamicPropertyRegistry registry) {
    registry.add("test.postgres.url", POSTGRES::getJdbcUrl);
    registry.add("test.postgres.username", POSTGRES::getUsername);
    registry.add("test.postgres.password", POSTGRES::getPassword);
    registry.add("test.smtp.port", () -> SMTP.getSmtp().getPort());
  }
}
