/**
 * Minimal static server for visual tests — serves web/static with no build step.
 */
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(__dirname, '../static');
const PORT = Number(process.env.PORT || 8765);

const MIME = {
  '.html': 'text/html',
  '.css': 'text/css',
  '.js': 'application/javascript',
  '.mjs': 'application/javascript',
  '.json': 'application/json',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.woff2': 'font/woff2',
  '.md': 'text/plain',
};

const server = http.createServer((req, res) => {
  let urlPath = decodeURIComponent(req.url.split('?')[0]);
  if (urlPath === '/') urlPath = '/index.html';

  let filePath = path.join(ROOT, urlPath);

  if (!filePath.startsWith(ROOT)) {
    res.writeHead(403);
    res.end('Forbidden');
    return;
  }

  const tryRead = (target, onMissing) => {
    fs.stat(target, (statErr, stat) => {
      if (statErr) {
        onMissing();
        return;
      }
      let readPath = target;
      if (stat.isDirectory()) {
        readPath = path.join(target, 'index.html');
      }
      fs.readFile(readPath, (err, data) => {
        if (err) {
          onMissing();
          return;
        }
        const ext = path.extname(readPath);
        res.writeHead(200, { 'Content-Type': MIME[ext] || 'application/octet-stream' });
        res.end(data);
      });
    });
  };

  tryRead(filePath, () => {
    if (!urlPath.endsWith('.html')) {
      tryRead(`${filePath}.html`, () => {
        res.writeHead(404);
        res.end('Not found');
      });
      return;
    }
    res.writeHead(404);
    res.end('Not found');
  });
});

server.listen(PORT, () => {
  console.log(`Static server: http://127.0.0.1:${PORT}`);
});
