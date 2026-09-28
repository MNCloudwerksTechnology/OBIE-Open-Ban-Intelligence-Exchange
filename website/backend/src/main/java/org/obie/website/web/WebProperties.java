package org.obie.website.web;

import jakarta.validation.constraints.NotNull;
import jakarta.validation.constraints.Pattern;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.util.unit.DataSize;
import org.springframework.validation.annotation.Validated;

/**
 * HTTP settings of the website ({@code obie.web.*}).
 *
 * @param siteOrigin the site's own origin, e.g. {@code https://obie.example}; the only origin
 *     allowed to call the API cross-origin, and the base of the canonical URLs, the share image and
 *     the sitemap. Only scheme, host and port, so it can be written into pages verbatim
 * @param maxRequestBody larger request bodies are rejected with 413
 */
@Validated
@ConfigurationProperties("obie.web")
public record WebProperties(
    @NotNull
        @Pattern(
            regexp = "https?://(\\[[0-9A-Fa-f:.]+\\]|[A-Za-z0-9.-]+)(:[0-9]{1,5})?",
            message = "must be an origin such as https://obie.example (no path, no slash)")
        String siteOrigin,
    @NotNull DataSize maxRequestBody) {}
