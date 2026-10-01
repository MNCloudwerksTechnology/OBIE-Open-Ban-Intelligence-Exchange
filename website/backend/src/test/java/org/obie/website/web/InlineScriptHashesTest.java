package org.obie.website.web;

import static org.assertj.core.api.Assertions.assertThat;

import java.util.Set;
import org.junit.jupiter.api.Test;

class InlineScriptHashesTest {

  // printf 'alert(1)' | openssl dgst -sha256 -binary | base64
  private static final String ALERT_HASH = "'sha256-bhHHL3z2vDgxUt0W3dWQOrprscmda2Y5pLsLg4GF+pI='";

  @Test
  void hashesInlineScripts() {
    assertThat(InlineScriptHashes.inHtml("<p>x</p><script>alert(1)</script>"))
        .containsExactly(ALERT_HASH);
  }

  @Test
  void hashesExecutableTypesOnly() {
    String html =
        """
        <script type="module">alert(1)</script>
        <script type="text/javascript">alert(1)</script>
        <script id="ng-state" type="application/json">{"a":1}</script>
        <script type="importmap">{"imports":{}}</script>
        """;

    assertThat(InlineScriptHashes.inHtml(html)).containsExactly(ALERT_HASH);
  }

  @Test
  void ignoresExternalAndEmptyScripts() {
    String html = "<script src=\"main.js\" type=\"module\"></script><script></script>";

    assertThat(InlineScriptHashes.inHtml(html)).isEmpty();
  }

  @Test
  void contentSecurityPolicyListsTheHashesInScriptSrc() {
    String policy = SecurityHeadersFilter.contentSecurityPolicy(Set.of(ALERT_HASH));

    assertThat(policy)
        .contains(
            "script-src 'self' " + SecurityHeadersFilter.ANALYTICS_ORIGIN + " " + ALERT_HASH + ";")
        .doesNotContain("script-src 'self' 'unsafe-inline'");
  }

  @Test
  void contentSecurityPolicyWithoutInlineScripts() {
    assertThat(SecurityHeadersFilter.contentSecurityPolicy(Set.of()))
        .contains("script-src 'self' " + SecurityHeadersFilter.ANALYTICS_ORIGIN + ";");
  }
}
