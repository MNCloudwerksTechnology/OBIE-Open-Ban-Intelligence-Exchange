package org.obie.website.web;

import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import java.io.IOException;
import java.io.InputStream;
import java.io.UncheckedIOException;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.Optional;
import org.springframework.boot.autoconfigure.web.servlet.error.ErrorViewResolver;
import org.springframework.core.io.Resource;
import org.springframework.core.io.ResourceLoader;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.stereotype.Component;
import org.springframework.web.servlet.ModelAndView;
import org.springframework.web.servlet.View;

/**
 * Answers 404 errors for HTML clients with the prerendered not-found page ({@code /404}), keeping
 * the 404 status and the configured origin ({@link SiteOrigin}). Other errors and non-HTML clients
 * fall through to Spring Boot's defaults.
 */
@Component
public class NotFoundPageResolver implements ErrorViewResolver {

  static final String NOT_FOUND_PAGE = StaticSiteConfig.STATIC_LOCATION + "404/index.html";

  private final ResourceLoader resourceLoader;
  private final SiteOrigin siteOrigin;

  /** The page with the origin filled in, read on first use; unknown URLs are frequent. */
  private volatile String page;

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
    Optional<String> html = page();
    if (html.isEmpty()) {
      return null;
    }
    ModelAndView view = new ModelAndView(new HtmlView(html.get()));
    view.setStatus(status);
    return view;
  }

  private Optional<String> page() {
    String html = page;
    if (html == null) {
      Resource resource = resourceLoader.getResource(NOT_FOUND_PAGE);
      if (!resource.isReadable()) {
        return Optional.empty();
      }
      try (InputStream in = resource.getInputStream()) {
        html = siteOrigin.applyTo(new String(in.readAllBytes(), StandardCharsets.UTF_8));
      } catch (IOException e) {
        throw new UncheckedIOException("Cannot read " + NOT_FOUND_PAGE, e);
      }
      page = html;
    }
    return Optional.of(html);
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
