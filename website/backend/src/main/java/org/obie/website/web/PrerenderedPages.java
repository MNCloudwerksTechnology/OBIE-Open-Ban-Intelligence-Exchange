package org.obie.website.web;

import java.io.IOException;
import java.io.UncheckedIOException;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.List;
import org.springframework.core.io.Resource;
import org.springframework.core.io.support.PathMatchingResourcePatternResolver;
import org.springframework.core.io.support.ResourcePatternResolver;

/**
 * The public paths of the prerendered pages, found as {@code <route>/index.html} under a static
 * location. The not-found page is not public. Reading them from the packaged front end means a new
 * page appears in the sitemap without a back-end change.
 */
final class PrerenderedPages {

  private static final String INDEX = "index.html";
  private static final String NOT_FOUND_DIRECTORY = "404/";

  private PrerenderedPages() {}

  /** Paths such as {@code /} and {@code /impressum}, the home page first, then alphabetical. */
  static List<String> publicPaths(String location) {
    ResourcePatternResolver resolver = new PathMatchingResourcePatternResolver();
    try {
      String root = resolver.getResource(location).getURL().toString();
      List<String> paths = new ArrayList<>();
      for (Resource page : resolver.getResources(location + "**/" + INDEX)) {
        String url = page.getURL().toString();
        if (!url.startsWith(root)) {
          continue; // the same path in another classpath root
        }
        String directory = url.substring(root.length(), url.length() - INDEX.length());
        if (!directory.equals(NOT_FOUND_DIRECTORY)) {
          paths.add("/" + stripTrailingSlash(directory));
        }
      }
      paths.sort(
          Comparator.comparing((String path) -> !path.equals("/")).thenComparing(path -> path));
      return List.copyOf(paths);
    } catch (IOException e) {
      throw new UncheckedIOException("Cannot list the prerendered pages in " + location, e);
    }
  }

  private static String stripTrailingSlash(String directory) {
    return directory.endsWith("/") ? directory.substring(0, directory.length() - 1) : directory;
  }
}
