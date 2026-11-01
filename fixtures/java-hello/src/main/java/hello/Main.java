package hello;

import com.sun.net.httpserver.HttpServer;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;

public final class Main {
  public static void main(String[] args) throws Exception {
    int port = Integer.parseInt(System.getenv().getOrDefault("PORT", "8080"));
    HttpServer server = HttpServer.create(new InetSocketAddress("0.0.0.0", port), 0);

    server.createContext("/health", exchange -> respond(exchange, "application/json", "{\"ok\":true}"));
    server.createContext("/", exchange -> respond(exchange, "text/plain", "hello\n"));

    server.start();
  }

  private static void respond(com.sun.net.httpserver.HttpExchange exchange, String type, String text)
      throws java.io.IOException {
    byte[] body = text.getBytes(StandardCharsets.UTF_8);
    exchange.getResponseHeaders().add("Content-Type", type);
    exchange.sendResponseHeaders(200, body.length);
    try (OutputStream out = exchange.getResponseBody()) {
      out.write(body);
    }
  }
}
