package org.obie.website.web;

import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import java.io.IOException;
import java.io.InputStream;
import java.util.Map;
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
 * the 404 status. Other errors and non-HTML clients fall through to Spring Boot's defaults.
 */
@Component
public class NotFoundPageResolver implements ErrorViewResolver {

  static final String NOT_FOUND_PAGE = StaticSiteConfig.STATIC_LOCATION + "404/index.html";

  private final ResourceLoader resourceLoader;

  public NotFoundPageResolver(ResourceLoader resourceLoader) {
    this.resourceLoader = resourceLoader;
  }

  @Override
  public ModelAndView resolveErrorView(
      HttpServletRequest request, HttpStatus status, Map<String, Object> model) {
    if (status != HttpStatus.NOT_FOUND) {
      return null;
    }
    Resource page = resourceLoader.getResource(NOT_FOUND_PAGE);
    if (!page.isReadable()) {
      return null;
    }
    ModelAndView view = new ModelAndView(new HtmlResourceView(page));
    view.setStatus(status);
    return view;
  }

  /** Writes a static HTML resource as the response body. */
  private record HtmlResourceView(Resource resource) implements View {

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
      try (InputStream in = resource.getInputStream()) {
        in.transferTo(response.getOutputStream());
      }
    }
  }
}
