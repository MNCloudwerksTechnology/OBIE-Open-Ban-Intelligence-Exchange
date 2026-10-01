package org.obie.website.web;

import java.io.IOException;
import java.io.InputStream;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.Base64;
import java.util.Locale;
import java.util.Set;
import java.util.TreeSet;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import org.springframework.core.io.Resource;
import org.springframework.core.io.support.PathMatchingResourcePatternResolver;
import org.springframework.core.io.support.ResourcePatternResolver;

/**
 * CSP source expressions ({@code 'sha256-…'}) for every inline script in the prerendered pages. The
 * Angular build writes a few small inline scripts (such as the loader of non-critical CSS);
 * allowing exactly their hashes keeps {@code 'unsafe-inline'} out of {@code script-src} without
 * coupling the back end to the front end's build details.
 */
final class InlineScriptHashes {

  private static final Pattern SCRIPT =
      Pattern.compile(
          "<script(?<attributes>[^>]*)>(?<body>.*?)</script>",
          Pattern.DOTALL | Pattern.CASE_INSENSITIVE);
  private static final Pattern SRC = Pattern.compile("\\ssrc\\s*=", Pattern.CASE_INSENSITIVE);
  private static final Pattern TYPE =
      Pattern.compile("\\stype\\s*=\\s*[\"']?(?<type>[^\"'\\s>]+)", Pattern.CASE_INSENSITIVE);

  /** Types the browser executes; others (such as application/json state) are data. */
  private static final Set<String> SCRIPT_TYPES =
      Set.of("module", "text/javascript", "application/javascript");

  private InlineScriptHashes() {}

  /** Hashes of the inline scripts in all HTML files under {@code location}, sorted. */
  static Set<String> of(String location) {
    ResourcePatternResolver resolver = new PathMatchingResourcePatternResolver();
    Set<String> hashes = new TreeSet<>();
    try {
      for (Resource page : resolver.getResources(location + "**/*.html")) {
        try (InputStream in = page.getInputStream()) {
          hashes.addAll(inHtml(new String(in.readAllBytes(), StandardCharsets.UTF_8)));
        }
      }
    } catch (IOException e) {
      throw new UncheckedIOException("Cannot read the prerendered pages in " + location, e);
    }
    return hashes;
  }

  /** Hashes of the executable inline scripts in one HTML document. */
  static Set<String> inHtml(String html) {
    Set<String> hashes = new TreeSet<>();
    Matcher script = SCRIPT.matcher(html);
    while (script.find()) {
      String attributes = script.group("attributes");
      String body = script.group("body");
      if (!body.isEmpty() && !SRC.matcher(attributes).find() && isExecutable(attributes)) {
        hashes.add("'sha256-" + sha256(body) + "'");
      }
    }
    return hashes;
  }

  private static boolean isExecutable(String attributes) {
    Matcher type = TYPE.matcher(attributes);
    return !type.find() || SCRIPT_TYPES.contains(type.group("type").toLowerCase(Locale.ROOT));
  }

  private static String sha256(String text) {
    try {
      byte[] digest =
          MessageDigest.getInstance("SHA-256").digest(text.getBytes(StandardCharsets.UTF_8));
      return Base64.getEncoder().encodeToString(digest);
    } catch (NoSuchAlgorithmException e) {
      // Every Java runtime ships SHA-256, so this cannot happen.
      throw new IllegalStateException(e);
    }
  }
}
