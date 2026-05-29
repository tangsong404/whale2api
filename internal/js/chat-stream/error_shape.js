'use strict';

function writeOpenAIError(res, status, message) {
  res.statusCode = status;
  res.setHeader('Content-Type', 'application/json');
  res.end(
    JSON.stringify({
      error: {
        message,
        type: openAIErrorType(status),
      },
    }),
  );
}

function openAIErrorType(status) {
  switch (status) {
    case 400:
      return 'invalid_request_error';
    case 401:
      return 'authentication_error';
    case 403:
      return 'permission_error';
    case 429:
      return 'rate_limit_error';
    case 503:
      return 'service_unavailable_error';
    default:
      return status >= 500 ? 'api_error' : 'invalid_request_error';
  }
}

module.exports = {
  writeOpenAIError,
  openAIErrorType,
};
