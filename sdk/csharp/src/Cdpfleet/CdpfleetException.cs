using System.Text.Json;

namespace Cdpfleet;

/// <summary>A non-success answer from the cdpfleet API (or a launch that kept failing on the network).</summary>
public class CdpfleetException : Exception
{
    /// <summary>HTTP status; 0 for a network failure.</summary>
    public int Status { get; }

    /// <summary>The <c>error</c> field of the response body, e.g. <c>daily_quota_exhausted</c>.</summary>
    public string? Code { get; }

    /// <summary>The parsed response body, if any.</summary>
    public JsonElement? Body { get; }

    /// <summary>Creates the exception from a status and a parsed body.</summary>
    public CdpfleetException(int status, JsonElement? body, string? message = null)
        : base(message ?? $"cdpfleet: {status} {ErrorOf(body)}".TrimEnd())
    {
        Status = status;
        Code = ErrorOf(body);
        Body = body;
    }

    internal static string? ErrorOf(JsonElement? body) =>
        body is { ValueKind: JsonValueKind.Object } b && b.TryGetProperty("error", out var e) && e.ValueKind == JsonValueKind.String
            ? e.GetString()
            : null;
}
