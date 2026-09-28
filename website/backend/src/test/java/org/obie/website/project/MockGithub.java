package org.obie.website.project;

import com.sun.net.httpserver.Headers;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.OutputStream;
import java.io.UncheckedIOException;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;

/**
 * A stand-in for the GitHub REST API on a random local port. Every path answers with the response
 * set for it (404 by default); received requests are recorded.
 */
final class MockGithub implements AutoCloseable {

  static final String REPOSITORY = "MNCloudwerksTechnology/OBIE-Open-Ban-Intelligence-Exchange";
  static final String REPOSITORY_PATH = "/repos/" + REPOSITORY;
  static final String WEB_URL = "https://github.com/" + REPOSITORY;

  static final String REPOSITORY_JSON =
      """
      {"id": 1, "full_name": "{REPOSITORY}", "html_url": "{WEB_URL}", "stargazers_count": 42,
       "forks_count": 7, "open_issues_count": 3, "default_branch": "main",
       "owner": {"login": "MNCloudwerksTechnology"}}
      """
          .replace("{REPOSITORY}", REPOSITORY)
          .replace("{WEB_URL}", WEB_URL);

  static final String RELEASE_JSON =
      """
      {"tag_name": "v0.1.0", "name": "OBIE 0.1.0", "published_at": "2026-09-01T12:00:00Z",
       "html_url": "{WEB_URL}/releases/tag/v0.1.0", "draft": false}
      """
          .replace("{WEB_URL}", WEB_URL);

  static final String COMMITS_JSON =
      """
      [{"sha": "abc123", "commit": {"author": {"date": "2026-09-19T08:00:00Z"},
        "committer": {"date": "2026-09-20T10:30:00Z"}, "message": "Merge"}}]
      """;

  private record Response(int status, String body, Map<String, String> headers, long delayMs) {}

  private final HttpServer server;
  private final Map<String, Response> responses = new ConcurrentHashMap<>();
  private final List<Headers> requests = new CopyOnWriteArrayList<>();

  private MockGithub(HttpServer server) {
    this.server = server;
    server.createContext("/", this::handle);
    server.start();
  }

  static MockGithub start() {
    try {
      return new MockGithub(
          HttpServer.create(new InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0));
    } catch (IOException e) {
      throw new UncheckedIOException(e);
    }
  }

  URI url() {
    return URI.create("http://127.0.0.1:" + server.getAddress().getPort());
  }

  /** Answers like the real repository: stats, a release and a commit. */
  MockGithub healthy() {
    responses.clear();
    respond(REPOSITORY_PATH, 200, REPOSITORY_JSON);
    respond(REPOSITORY_PATH + "/releases/latest", 200, RELEASE_JSON);
    respond(REPOSITORY_PATH + "/commits", 200, COMMITS_JSON);
    return this;
  }

  /** Answers every request like GitHub does once the rate limit is used up. */
  MockGithub rateLimited() {
    responses.clear();
    Response limited =
        new Response(
            403,
            "{\"message\": \"API rate limit exceeded\"}",
            Map.of("x-ratelimit-remaining", "0"),
            0);
    responses.put(REPOSITORY_PATH, limited);
    responses.put(REPOSITORY_PATH + "/releases/latest", limited);
    responses.put(REPOSITORY_PATH + "/commits", limited);
    return this;
  }

  MockGithub respond(String path, int status, String body) {
    responses.put(path, new Response(status, body, Map.of(), 0));
    return this;
  }

  MockGithub respondSlowly(String path, String body, long delayMs) {
    responses.put(path, new Response(200, body, Map.of(), delayMs));
    return this;
  }

  List<Headers> requests() {
    return List.copyOf(requests);
  }

  List<String> requestedUris() {
    return requests.stream().map(headers -> headers.getFirst("X-Test-Uri")).toList();
  }

  private void handle(HttpExchange exchange) throws IOException {
    Headers headers = new Headers();
    headers.putAll(exchange.getRequestHeaders());
    headers.set("X-Test-Uri", exchange.getRequestURI().toString());
    requests.add(headers);
    Response response =
        responses.getOrDefault(
            exchange.getRequestURI().getPath(),
            new Response(404, "{\"message\": \"Not Found\"}", Map.of(), 0));
    if (response.delayMs() > 0) {
      try {
        Thread.sleep(response.delayMs());
      } catch (InterruptedException e) {
        Thread.currentThread().interrupt();
      }
    }
    byte[] body = response.body().getBytes(StandardCharsets.UTF_8);
    exchange.getResponseHeaders().set("Content-Type", "application/json; charset=utf-8");
    response.headers().forEach(exchange.getResponseHeaders()::set);
    exchange.sendResponseHeaders(response.status(), body.length);
    try (OutputStream out = exchange.getResponseBody()) {
      out.write(body);
    }
  }

  @Override
  public void close() {
    server.stop(0);
  }
}
