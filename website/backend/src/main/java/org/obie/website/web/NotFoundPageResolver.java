package org.obie.website.web;

import jakarta.servlet.RequestDispatcher;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import java.io.IOException;
import java.io.InputStream;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.ConcurrentHashMap;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import org.springframework.boot.autoconfigure.web.servlet.error.ErrorViewResolver;
import org.springframework.core.io.Resource;
import org.springframework.core.io.ResourceLoader;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.stereotype.Component;
import org.springframework.web.servlet.ModelAndView;
import org.springframework.web.servlet.View;

/**
 * Answers 404 errors for HTML clients with the prerendered not-found page, keeping the 404 status
 * and the configured origin ({@link SiteOrigin}). An unknown URL under a language's prefix, such as
 * {@code /de/…}, gets that language's page ({@code /de/404}, ADR 0033); every other one gets {@code
 * /404}. Other errors and non-HTML clients fall through to Spring Boot's defaults.
 */
@Component
public class NotFoundPageResolver implements ErrorViewResolver {

  static final String NOT_FOUND_PAGE = StaticSiteConfig.STATIC_LOCATION + "404/index.html";

  /** The first path segment, if it can be a language prefix (two lower-case letters). */
  private static final Pattern LANGUAGE_PREFIX = Pattern.compile("^/([a-z]{2})(?:/|$)");

  private static final String DEFAULT = "";

  private final ResourceLoader resourceLoader;
  private final SiteOrigin siteOrigin;

  /**
   * The page per language prefix ({@code ""} for the default), with the origin filled in, read on
   * first use; unknown URLs are frequent. Empty where a prefix has no not-found page of its own.
   */
  private final Map<String, Optional<String>> pages = new ConcurrentHashMap<>();

  public NotFoundPageResolver(ResourceLoader resourceLoader, SiteOrigin siteOrigin) {
    this.resourceLoader = resourceLoader;
    this.siteOrigin = siteOrigin;
  }

  @Override
  public ModelAndView resolveErrorView(
      HttpServletRequest request, HttpStatus status, Map<String, Object> model) {
    if (status != HttpStatus.NOT_FOUND) {
      return null;
    }
    Optional<String> html = page(languagePrefix(request, model)).or(() -> page(DEFAULT));
    if (html.isEmpty()) {
      return null;
    }
    ModelAndView view = new ModelAndView(new HtmlView(html.get()));
    view.setStatus(status);
    return view;
  }

  /** The language prefix of the URL that was not found, or {@link #DEFAULT}. */
  private static String languagePrefix(HttpServletRequest request, Map<String, Object> model) {
    Object uri = request.getAttribute(RequestDispatcher.ERROR_REQUEST_URI);
    if (uri == null) {
      uri = model.get("path");
    }
    Matcher matcher = LANGUAGE_PREFIX.matcher(uri instanceof String path ? path : "");
    return matcher.find() ? matcher.group(1) : DEFAULT;
  }

  private Optional<String> page(String prefix) {
    return pages.computeIfAbsent(prefix, this::read);
  }

  private Optional<String> read(String prefix) {
    String location = prefix.isEmpty() ? NOT_FOUND_PAGE : location(prefix);
    Resource resource = resourceLoader.getResource(location);
    if (!resource.isReadable()) {
      return Optional.empty();
    }
    try (InputStream in = resource.getInputStream()) {
      return Optional.of(siteOrigin.applyTo(new String(in.readAllBytes(), StandardCharsets.UTF_8)));
    } catch (IOException e) {
      throw new UncheckedIOException("Cannot read " + location, e);
    }
  }

  private static String location(String prefix) {
    return StaticSiteConfig.STATIC_LOCATION
        + prefix
        + "/"
        + PrerenderedPages.NOT_FOUND_DIRECTORY
        + "index.html";
  }

  /** Writes a fixed HTML document as the response body. */
  private record HtmlView(String html) implements View {

    @Override
    public String getContentType() {
      return MediaType.TEXT_HTML_VALUE;
    }

    @Override
    public void render(
        Map<String, ?> model, HttpServletRequest request, HttpServletResponse response)
        throws IOException {
      response.setContentType(MediaType.TEXT_HTML_VALUE);
      response.setCharacterEncoding("UTF-8");
      response.getWriter().write(html);
    }
  }
}
