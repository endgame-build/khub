import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { readFile } from 'node:fs/promises';
const code = await readFile(new URL('../ui/shell.js', import.meta.url), 'utf8');
const { word, verifyCommand } = await import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'));

const hostile = [
  "x; touch PWNED; #", 'z\ntouch NL', "it's", '$(id)', '`id`', 'a b', '--help me', 'é', "'; id; '", '*', '~', ''
];

test('a plain id is copied as it reads', () => {
  assert.equal(word('requirement/req-an-invoice'), 'requirement/req-an-invoice');
  assert.equal(verifyCommand('requirement/req-a'), 'khub verify requirement/req-a --by human:<name>');
});

test('a hostile id is one quoted word', () => {
  assert.equal(word("x; rm it's"), "'x; rm it'\\''s'");
  hostile.forEach(id => assert.match(word(id), /^'[\s\S]*'$/, JSON.stringify(id)));
});

test('a shell hands every id back unchanged', { skip: process.platform === 'win32' }, () => {
  hostile.forEach(id => {
    const out = execFileSync('sh', ['-c', 'printf %s ' + word(id)], { encoding: 'utf8' });
    assert.equal(out, id);
  });
});

test('the verify command runs nothing until a name is put in', { skip: process.platform === 'win32' }, () => {
  const run = cmd => spawnSync('sh', ['-c', 'khub() { echo RAN "$@"; }; ' + cmd], { encoding: 'utf8' });
  const asCopied = run(verifyCommand('requirement/req-a'));
  assert.notEqual(asCopied.status, 0);
  assert.equal(asCopied.stdout, '');
  assert.equal(run(verifyCommand('requirement/req-a').replace('<name>', 'ana')).stdout,
    'RAN verify requirement/req-a --by human:ana\n');
});
