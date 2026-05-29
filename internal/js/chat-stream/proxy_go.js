'use strict';

const {
  buildInternalGoURL,
  buildInternalGoHeaders,
  isAbortError,
} = require('./http_internal');

async function proxyToGo(req, res, rawBody) {
  const url = buildInternalGoURL(req);
  const controller = new AbortController();
  let clientClosed = false;
  const markClientClosed = () => {
    if (clientClosed) {
      return;
    }
    clientClosed = true;
    controller.abort();
  };
  const onReqAborted = () => markClientClosed();
  const onResClose = () => {
    if (!res.writableEnded) {
      markClientClosed();
    }
  };
  req.on('aborted', onReqAborted);
  res.on('close', onResClose);

  try {
    let upstream;
    try {
      upstream = await fetch(url.toString(), {
        method: 'POST',
        headers: buildInternalGoHeaders(req, { withContentType: true }),
        body: rawBody,
        signal: controller.signal,
      });
    } catch (err) {
      if (clientClosed || isAbortError(err)) {
        if (!res.writableEnded) {
          res.end();
        }
        return;
      }
      throw err;
    }
    if (clientClosed) {
      if (!res.writableEnded) {
        res.end();
      }
      return;
    }

    res.statusCode = upstream.status;
    upstream.headers.forEach((value, key) => {
      const lower = key.toLowerCase();
      if (lower === 'content-length' || lower === 'content-encoding') {
        return;
      }
      res.setHeader(key, value);
    });

    if (!upstream.body || typeof upstream.body.getReader !== 'function') {
      const bytes = Buffer.from(await upstream.arrayBuffer());
      res.end(bytes);
      return;
    }

    const reader = upstream.body.getReader();
    try {
      // eslint-disable-next-line no-constant-condition
      while (true) {
        if (clientClosed) {
          break;
        }
        const { value, done } = await reader.read();
        if (done) {
          break;
        }
        if (value && value.length > 0) {
          res.write(Buffer.from(value));
          if (typeof res.flush === 'function') {
            res.flush();
          }
        }
      }
      if (!res.writableEnded) {
        res.end();
      }
    } catch (err) {
      if (!isAbortError(err) && !res.writableEnded) {
        res.end();
      }
    }
  } finally {
    req.removeListener('aborted', onReqAborted);
    res.removeListener('close', onResClose);
    if (!res.writableEnded) {
      res.end();
    }
  }
}

module.exports = {
  proxyToGo,
};
