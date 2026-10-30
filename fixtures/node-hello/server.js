const http = require("node:http");

const port = Number(process.env.PORT || 8080);

const server = http.createServer((req, res) => {
  if (req.url === "/health") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end('{"ok":true}');
    return;
  }
  res.writeHead(200, { "Content-Type": "text/plain" });
  res.end("hello\n");
});

server.listen(port, () => {
  console.log(`listening on ${port}`);
});
