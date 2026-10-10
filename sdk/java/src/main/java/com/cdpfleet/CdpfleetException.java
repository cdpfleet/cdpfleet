package com.cdpfleet;

import com.google.gson.JsonElement;
import com.google.gson.JsonObject;

/** A cdpfleet API error: the HTTP status, the {@code error} code from the body and the body itself. */
public class CdpfleetException extends RuntimeException {
  private final int status;
  private final String code;
  private final JsonElement body;

  public CdpfleetException(int status, JsonElement body) {
    this(status, body, null);
  }

  public CdpfleetException(int status, JsonElement body, String message) {
    super(message != null ? message : ("cdpfleet: " + status + " " + (codeOf(body) == null ? "" : codeOf(body))).trim());
    this.status = status;
    this.code = codeOf(body);
    this.body = body;
  }

  static CdpfleetException of(int status, String code, String message) {
    JsonObject body = new JsonObject();
    body.addProperty("error", code);
    return new CdpfleetException(status, body, message);
  }

  static String codeOf(JsonElement body) {
    if (body == null || !body.isJsonObject()) return null;
    JsonElement e = body.getAsJsonObject().get("error");
    return e != null && e.isJsonPrimitive() ? e.getAsString() : null;
  }

  /** HTTP status, or 0 for a network error. */
  public int status() { return status; }

  /** The {@code error} field of the response body (e.g. {@code threads_exceeded}), or null. */
  public String code() { return code; }

  /** The parsed response body, or null. */
  public JsonElement body() { return body; }
}
