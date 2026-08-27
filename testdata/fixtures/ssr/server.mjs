import http from 'node:http';
import crypto from 'node:crypto';

const port = Number(process.env.PORT || 4173);
const server = http.createServer((req, res) => {
  if (req.url === '/assets/app.css') { res.setHeader('Content-Type', 'text/css'); res.end('body{color:#123456}'); return; }
  if (req.url === '/redirect') { res.writeHead(302, { Location: '/target' }).end(); return; }
  if (req.url === '/cookie') { res.writeHead(200, { 'Set-Cookie': 'devlan_fixture=ok; Path=/' }).end('cookie'); return; }
  if (req.url === '/origin') { res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify({ host: req.headers.host, origin: req.headers.origin || '' })); return; }
  res.setHeader('Content-Type', 'text/html; charset=utf-8'); res.end(`<!doctype html><title>SSR</title><a href="/assets/app.css">asset</a><main>${req.headers.host}</main>`);
});
server.on('upgrade', (req, socket) => {
  if (req.headers.upgrade?.toLowerCase() !== 'websocket') return socket.destroy();
  const accept = crypto.createHash('sha1').update(req.headers['sec-websocket-key'] + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');
  socket.write('HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ' + accept + '\r\n\r\n');
  socket.end();
});
server.listen(port, '127.0.0.1');
