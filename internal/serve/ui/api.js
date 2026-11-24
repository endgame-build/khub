// api.js — the only module that fetches.
//
// serve's error envelope is `{error:{code,message}}` (internal/serve/serve.go
// `fail`), emitted with a real HTTP status. One handler unwraps it so no caller
// has to know the shape.

function get(path, signal) {
  return fetch(path, { headers: { 'Accept': 'application/json' }, signal: signal })
    .catch(function (cause) {
      if (cause && cause.name === 'AbortError') throw cause; // the caller's own doing

      // The browser's own text ("Failed to fetch", "Load failed") says
      // nothing; the usual cause is that the operator stopped the server.
      var err = new Error('khub serve is not responding — is it still running?');
      err.code = 'unreachable';
      throw err;
    })
    .then(function (r) {
      return r.json().catch(function () {
        var err = new Error('HTTP ' + r.status + ': the server sent something other than JSON');
        err.status = r.status;
        throw err;
      }).then(function (body) {
        if (body && body.error) {
          var err = new Error(body.error.message || 'request failed');
          err.code = body.error.code;
          err.status = r.status;
          err.expected = body.error.expected || [];
          err.suggestion = body.error.suggestion || '';
          throw err;
        }
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return body;
      });
    });
}

function idPath(id) { return id.split('/').map(encodeURIComponent).join('/'); }

export function getOverview() { return get('/api/overview'); }
export function getFindings() { return get('/api/findings'); }
export function getRecent(days, limit) { return get('/api/recent?days=' + encodeURIComponent(days) + (limit ? '&limit=' + limit : '')); }
export function getQueries() { return get('/api/queries'); }
export function getQuery(name) { return get('/api/query/' + encodeURIComponent(name)); }
export function getMarked(name) { return get('/api/marked/' + encodeURIComponent(name)); }
export function search(q, signal) { return get('/api/search?q=' + encodeURIComponent(q), signal); }
export function getSchema() { return get('/api/schema'); }
export function getType(name) { return get('/api/type/' + encodeURIComponent(name)); }
export function getRows(type) { return get('/api/rows/' + encodeURIComponent(type)); }
export function getEntity(id) { return get('/api/entity/' + idPath(id)); }
export function getHistory(id) { return get('/api/history/' + idPath(id)); }
export function getReach(id) { return get('/api/reach/' + idPath(id)); }
