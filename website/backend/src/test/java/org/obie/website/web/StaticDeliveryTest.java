package org.obie.website.web;

import static org.assertj.core.api.Assertions.assertThat;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.io.UncheckedIOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.zip.GZIPInputStream;
import org.junit.jupiter.api.Test;
import org.obie.website.IntegrationTest;
import org.springframework.boot.test.web.server.LocalServerPort;

/** Caching and compression of the front end, as a browser receives them. */
class StaticDeliveryTest extends IntegrationTest {

  private static final String ONE_YEAR_IMMUTABLE = "max-age=31536000, public, immutable";

  /** Does not decompress on its own, so the tests see the encoding on the wire. */
  private final HttpClient client = HttpClient.newHttpClient();

  @LocalServerPort private int port;

  @Test
  void hashedBundleIsCachedForAYearAndServedWithBrotli() {
    HttpResponse<byte[]> response = get("/" + mainBundle(), "br, gzip");

    assertThat(response.statusCode()).isEqualTo(200);
    assertThat(header(response, "Content-Encoding")).isEqualTo("br");
    assertThat(header(response, "Cache-Control")).isEqualTo(ONE_YEAR_IMMUTABLE);
    assertThat(header(response, "Vary")).contains("Accept-Encoding");
    assertThat(header(response, "Content-Type")).contains("javascript");
  }

  @Test
  void hashedBundleFallsBackToGzipAndToIdentity() {
    String bundle = "/" + mainBundle();
    HttpResponse<byte[]> gzip = get(bundle, "gzip");
    HttpResponse<byte[]> identity = get(bundle, "identity");

    assertThat(header(gzip, "Content-Encoding")).isEqualTo("gzip");
    assertThat(identity.headers().firstValue("Content-Encoding")).isEmpty();
    assertThat(gunzip(gzip.body())).isEqualTo(identity.body());
  }

  @Test
  void hashedFontsAreCachedForAYear() {
    Matcher font =
        Pattern.compile("href=\"(media/inter-latin-400-normal-[A-Z0-9]+\\.woff2)\"")
            .matcher(text(get("/", "identity")));
    assertThat(font.find()).as("preloaded font in the home page").isTrue();

    HttpResponse<byte[]> response = get("/" + font.group(1), "identity");

    assertThat(response.statusCode()).isEqualTo(200);
    assertThat(header(response, "Cache-Control")).isEqualTo(ONE_YEAR_IMMUTABLE);
  }

  @Test
  void pagesAreRevalidatedAndCompressedWithTheOriginFilledIn() {
    HttpResponse<byte[]> response = get("/", "br, gzip");

    assertThat(response.statusCode()).isEqualTo(200);
    assertThat(header(response, "Cache-Control")).isEqualTo("no-cache");
    // Pages are rewritten when served, so Tomcat compresses them with gzip.
    assertThat(header(response, "Content-Encoding")).isEqualTo("gzip");
    assertThat(new String(gunzip(response.body()), StandardCharsets.UTF_8))
        .contains("<link rel=\"canonical\" href=\"https://obie.example/\">");
  }

  @Test
  void unhashedFilesAreRevalidated() {
    HttpResponse<byte[]> response = get("/brand/obie-logo-solo.svg", "identity");

    assertThat(response.statusCode()).isEqualTo(200);
    assertThat(header(response, "Cache-Control")).isEqualTo("no-cache");
  }

  /** The hashed name of the main bundle, from the home page. */
  private String mainBundle() {
    Matcher main =
        Pattern.compile("src=\"(main-[A-Z0-9]{8}\\.js)\"").matcher(text(get("/", "identity")));
    assertThat(main.find()).as("main bundle in the home page").isTrue();
    return main.group(1);
  }

  private HttpResponse<byte[]> get(String path, String acceptEncoding) {
    HttpRequest request =
        HttpRequest.newBuilder(URI.create("http://localhost:" + port + path))
            .header("Accept", "text/html,*/*")
            .header("Accept-Encoding", acceptEncoding)
            .build();
    try {
      return client.send(request, HttpResponse.BodyHandlers.ofByteArray());
    } catch (IOException e) {
      throw new UncheckedIOException(e);
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw new IllegalStateException(e);
    }
  }

  private static String header(HttpResponse<?> response, String name) {
    return response.headers().firstValue(name).orElse(null);
  }

  private static String text(HttpResponse<byte[]> response) {
    return new String(response.body(), StandardCharsets.UTF_8);
  }

  private static byte[] gunzip(byte[] body) {
    try (GZIPInputStream in = new GZIPInputStream(new ByteArrayInputStream(body))) {
      return in.readAllBytes();
    } catch (IOException e) {
      throw new UncheckedIOException(e);
    }
  }
}
