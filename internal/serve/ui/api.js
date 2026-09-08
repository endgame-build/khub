// api.js — the only module that fetches.
//
// serve's error envelope is `{error:{code,message}}` (internal/serve/serve.go
// `fail`), emitted with a real HTTP status. One handler unwraps it so no caller
// has to know the shape.

function get(path) {
  return fetch(path, { headers: { 'Accept': 'application/json' } })
    .then(function (r) {
      return r.json().then(function (body) {
        if (body && body.error) {
          var err = new Error(body.error.message || 'request failed');
          err.code = body.error.code;
          err.status = r.status;
          throw err;
        }
        if (!r.ok) throw new Error('HTTP ' + r.status);
        return body;
      });
    });
}

export function getGraph() { return get('/api/graph'); }
export function getSchema() { return get('/api/schema'); }
export function getType(name) { return get('/api/type/' + encodeURIComponent(name)); }

export function getEntity(id) { return get('/api/entity/' + id.split('/').map(encodeURIComponent).join('/')); }
