package org.obie.website.inquiry;

import static org.assertj.core.api.Assertions.assertThat;
import static org.obie.website.inquiry.InquiryFixtures.post;
import static org.obie.website.inquiry.InquiryFixtures.validInquiry;

import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.time.LocalDate;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import java.util.stream.Stream;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.Arguments;
import org.junit.jupiter.params.provider.MethodSource;
import org.obie.website.IntegrationTest;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.web.client.TestRestTemplate;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;

/** {@code POST /api/inquiries} end to end: validation, bot handling, storage, error format. */
class InquiryApiTest extends IntegrationTest {

  /** A field value that stands for "leave the field out". */
  private static final Object ABSENT = new Object();

  @Autowired private TestRestTemplate http;
  @Autowired private ObjectMapper json;
  @Autowired private InquiryRepository repository;
  @Autowired private InquiryProperties properties;
  @Autowired private ClientIpHasher ipHasher;

  @Test
  void validInquiryIsStoredAndAcceptedWithItsId() throws JsonProcessingException {
    Map<String, Object> fields = validInquiry(properties);

    ResponseEntity<String> response = post(http, fields);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);
    JsonNode body = json.readTree(response.getBody());
    assertThat(body.properties()).hasSize(1);
    UUID id = UUID.fromString(body.get("id").asText());
    Inquiry stored = repository.findById(id).orElseThrow();
    assertThat(stored.getType()).isEqualTo(InquiryType.TALK);
    assertThat(stored.getName()).isEqualTo(fields.get("name"));
    assertThat(stored.getEmail()).isEqualTo("ada@example.org");
    assertThat(stored.getOrganisation()).isEqualTo("Analytical Engines Ltd");
    assertThat(stored.getEventDate()).isEqualTo(LocalDate.parse((String) fields.get("eventDate")));
    assertThat(stored.getEventLocation()).isEqualTo("online");
    assertThat(stored.getAudienceSize()).isEqualTo(120);
    assertThat(stored.getMessage()).isEqualTo(fields.get("message"));
    assertThat(stored.getStatus()).isEqualTo(InquiryStatus.NEW);
    assertThat(stored.getCreatedAt()).isNotNull();
  }

  @Test
  void clientIpIsStoredOnlyAsSaltedHash() throws JsonProcessingException {
    ResponseEntity<String> response = post(http, validInquiry(properties));

    Inquiry stored = repository.findById(idOf(response)).orElseThrow();
    assertThat(stored.getClientIpHash())
        .matches("[0-9a-f]{64}")
        .isEqualTo(ipHasher.hash("127.0.0.1"))
        .doesNotContain("127.0.0.1");
  }

  @Test
  void optionalFieldsMayBeLeftOut() throws JsonProcessingException {
    Map<String, Object> fields = validInquiry(properties);
    for (String optional :
        List.of("organisation", "eventDate", "eventLocation", "audienceSize", "website")) {
      fields.remove(optional);
    }

    ResponseEntity<String> response = post(http, fields);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);
    Inquiry stored = repository.findById(idOf(response)).orElseThrow();
    assertThat(stored.getOrganisation()).isNull();
    assertThat(stored.getEventDate()).isNull();
  }

  @ParameterizedTest(name = "{0} = {1}")
  @MethodSource("validValues")
  void acceptsValidValues(String field, Object value) {
    Map<String, Object> fields = validInquiry(properties);
    fields.put(field, value);

    assertThat(post(http, fields).getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);
  }

  static Stream<Arguments> validValues() {
    return Stream.of(
        Arguments.of("type", "workshop"),
        Arguments.of("type", "interview"),
        Arguments.of("type", "collaboration"),
        Arguments.of("type", "other"),
        Arguments.of("eventLocation", "Berlin, Germany"),
        Arguments.of("eventDate", LocalDate.now().plusDays(1).toString()),
        Arguments.of("message", "x".repeat(20)),
        Arguments.of("message", "x".repeat(5000)),
        Arguments.of("name", "Zoë Ångström-Łukasiewicz"));
  }

  @ParameterizedTest(name = "{0} = {1}")
  @MethodSource("invalidValues")
  void rejectsInvalidFieldWithMessageForThatField(String field, Object value, String message)
      throws JsonProcessingException {
    Map<String, Object> fields = validInquiry(properties);
    if (value == ABSENT) {
      fields.remove(field);
    } else {
      fields.put(field, value);
    }

    ResponseEntity<String> response = post(http, fields);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    assertThat(response.getHeaders().getContentType())
        .isEqualTo(MediaType.APPLICATION_PROBLEM_JSON);
    JsonNode body = json.readTree(response.getBody());
    assertThat(body.get("status").asInt()).isEqualTo(400);
    assertThat(body.get("errors"))
        .hasSize(1)
        .first()
        .satisfies(
            error -> {
              assertThat(error.get("field").asText()).isEqualTo(field);
              assertThat(error.get("message").asText()).isEqualTo(message);
            });
    assertNothingStoredFor(fields);
  }

  static Stream<Arguments> invalidValues() {
    String tooLong = "x".repeat(201);
    return Stream.of(
        Arguments.of("type", ABSENT, "Please choose what your inquiry is about."),
        Arguments.of(
            "type",
            "keynote",
            "Please choose one of: talk, workshop, interview, collaboration, other."),
        Arguments.of(
            "type",
            "TALK",
            "Please choose one of: talk, workshop, interview, collaboration, other."),
        Arguments.of("name", ABSENT, "Please enter your name."),
        Arguments.of("name", "   ", "Please enter your name."),
        Arguments.of("name", tooLong, "Please use at most 200 characters."),
        Arguments.of("name", "Ada\r\nBcc: victim@example.org", "Please use a single line."),
        Arguments.of("email", ABSENT, "Please enter your e-mail address."),
        Arguments.of("email", "not-an-address", "Please enter a valid e-mail address."),
        Arguments.of(
            "email",
            "a@example.org\nBcc: victim@example.org",
            "Please enter a valid e-mail address."),
        Arguments.of("organisation", tooLong, "Please use at most 200 characters."),
        Arguments.of("eventDate", "2020-01-01", "Please choose a date in the future."),
        Arguments.of(
            "eventDate", LocalDate.now().toString(), "Please choose a date in the future."),
        Arguments.of("eventDate", "31.12.2099", "Please enter a date as YYYY-MM-DD."),
        Arguments.of("eventLocation", tooLong, "Please use at most 200 characters."),
        Arguments.of("audienceSize", 0, "Please enter a positive number."),
        Arguments.of("audienceSize", -5, "Please enter a positive number."),
        Arguments.of("audienceSize", 1_000_001, "Please enter at most 1,000,000."),
        Arguments.of("audienceSize", "many", "This value has the wrong format."),
        Arguments.of("message", ABSENT, "Please enter a message."),
        Arguments.of("message", "x".repeat(19), "Please write between 20 and 5000 characters."),
        Arguments.of("message", "x".repeat(5001), "Please write between 20 and 5000 characters."),
        Arguments.of("consent", ABSENT, "Please accept the privacy notice."),
        Arguments.of("consent", false, "Please accept the privacy notice."),
        Arguments.of("consent", "true", "This value has the wrong format."),
        Arguments.of("formToken", ABSENT, "The form is out of date. Please reload the page."),
        Arguments.of("colour", "blue", "Unknown field."));
  }

  @Test
  void reportsEveryInvalidField() throws JsonProcessingException {
    Map<String, Object> fields = validInquiry(properties);
    fields.put("name", "");
    fields.put("email", "nope");
    fields.put("consent", false);

    JsonNode body = json.readTree(post(http, fields).getBody());

    assertThat(body.get("errors").findValuesAsText("field"))
        .containsExactly("consent", "email", "name");
  }

  @Test
  void malformedJsonGetsGenericErrorWithoutInternals() {
    ResponseEntity<String> response = post(http, "{\"type\": \"talk\",");

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.BAD_REQUEST);
    assertThat(response.getBody())
        .contains("The request body is not a valid JSON inquiry.")
        .doesNotContainIgnoringCase("jackson")
        .doesNotContainIgnoringCase("exception")
        .doesNotContain("org.obie");
  }

  @Test
  void nonJsonBodyIsRejected() {
    HttpHeaders headers = new HttpHeaders();
    headers.setContentType(MediaType.TEXT_PLAIN);
    ResponseEntity<String> response =
        http.postForEntity("/api/inquiries", new HttpEntity<>("type=talk", headers), String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.UNSUPPORTED_MEDIA_TYPE);
    assertThat(response.getBody()).doesNotContainIgnoringCase("exception");
  }

  @Test
  void filledHoneypotGetsFakeAcceptanceAndIsNotStored() throws JsonProcessingException {
    Map<String, Object> fields = validInquiry(properties);
    fields.put("website", "https://spam.example");

    assertFakeAcceptance(fields);
  }

  @Test
  void formFilledFasterThanMinimumFillTimeGetsFakeAcceptanceAndIsNotStored()
      throws JsonProcessingException {
    Map<String, Object> fields = validInquiry(properties);
    fields.put("formToken", freshToken());

    assertFakeAcceptance(fields);
  }

  @Test
  void forgedFormTokenGetsFakeAcceptanceAndIsNotStored() throws JsonProcessingException {
    Map<String, Object> fields = validInquiry(properties);
    fields.put("formToken", "1000.forged");

    assertFakeAcceptance(fields);
  }

  @Test
  void botsAreNotToldAboutValidationErrors() throws JsonProcessingException {
    Map<String, Object> fields = validInquiry(properties);
    fields.put("website", "filled");
    fields.put("email", "not-an-address");

    assertFakeAcceptance(fields);
  }

  @Test
  void formTokenIsFreshAndNotCached() throws JsonProcessingException {
    ResponseEntity<String> response = http.getForEntity("/api/inquiries/form-token", String.class);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.OK);
    assertThat(response.getHeaders().getCacheControl()).isEqualTo("no-store");
    String token = json.readTree(response.getBody()).get("token").asText();
    assertThat(token).matches("\\d+\\.[A-Za-z0-9_-]+");
  }

  @Test
  void bodyLargerThan16KibIsRejected() {
    Map<String, Object> fields = validInquiry(properties);
    fields.put("message", "x".repeat(16 * 1024));

    ResponseEntity<String> response = post(http, fields);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.PAYLOAD_TOO_LARGE);
    assertNothingStoredFor(fields);
  }

  private void assertFakeAcceptance(Map<String, Object> fields) throws JsonProcessingException {
    ResponseEntity<String> response = post(http, fields);

    assertThat(response.getStatusCode()).isEqualTo(HttpStatus.ACCEPTED);
    UUID id = idOf(response);
    assertThat(repository.existsById(id)).isFalse();
    assertNothingStoredFor(fields);
  }

  private void assertNothingStoredFor(Map<String, Object> fields) {
    Object name = fields.get("name");
    assertThat(repository.findAll()).noneMatch(inquiry -> inquiry.getName().equals(name));
  }

  private String freshToken() throws JsonProcessingException {
    String body = http.getForObject("/api/inquiries/form-token", String.class);
    return json.readTree(body).get("token").asText();
  }

  private UUID idOf(ResponseEntity<String> response) throws JsonProcessingException {
    return UUID.fromString(json.readTree(response.getBody()).get("id").asText());
  }
}
