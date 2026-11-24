import test from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { readFile } from 'node:fs/promises';
const code = await readFile(new URL('../ui/shell.js', import.meta.url), 'utf8');
const { word, verifyCommand, queryCommand, runCommand, reviewerFault } = await import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'));

const hostile = [
  "x; touch PWNED; #", "it's", '$(id)', '`id`', 'a b', '--help me', 'é', "'; id; '", '*', '~', ''
];

test('a plain id is copied as it reads', () => {
  assert.equal(word('requirement/req-an-invoice'), 'requirement/req-an-invoice');
  assert.equal(verifyCommand('requirement/req-a', 'ana'), 'khub verify requirement/req-a --by human:ana');
});

test('a hostile id is one quoted word', () => {
  assert.equal(word("x; rm it's"), "'x; rm it'\\''s'");
  hostile.forEach(id => assert.match(word(id), /^'[\s\S]*'$/, JSON.stringify(id)));
});

test('a value a terminal would act on is never copied', () => {
  ['z\ntouch NL', 'a\u001b[201~b', 'a\u0015b', 'a\u007f', 'a\u009bb', '\u0000'].forEach(v => {
    assert.equal(word(v), '', JSON.stringify(v));
    assert.equal(verifyCommand('note/' + v, 'ana'), '');
    assert.equal(runCommand(v), '');
    assert.equal(q({ attr: ['owner=' + v] }).command, '');
  });
  assert.equal(runCommand('open things'), "khub run 'open things'");
});

test('a shell hands every id back unchanged', { skip: process.platform === 'win32' }, () => {
  hostile.forEach(id => {
    const out = execFileSync('sh', ['-c', 'printf %s ' + word(id)], { encoding: 'utf8' });
    assert.equal(out, id);
  });
});

test('the verify command names one reviewer, whatever the name', { skip: process.platform === 'win32' }, () => {
  const run = cmd => spawnSync('sh', ['-c', 'khub() { printf "[%s]" "$@"; }; ' + cmd], { encoding: 'utf8' }).stdout;
  ['ana', 'ana-ruiz', "o'neil", '$(id)', 'x;touch', 'José'].forEach(name => {
    assert.equal(run(verifyCommand('requirement/req-a', name)), '[verify][requirement/req-a][--by][human:' + name + ']');
  });
  // nobody's review is not a command
  ['', '  ', undefined, 'a\u001bb', 'Ana Ruiz', 'ana ', 'ana\u00a0ruiz'].forEach(name => {
    assert.equal(verifyCommand('requirement/req-a', name), '', JSON.stringify(name));
    assert.notEqual(reviewerFault(name), '');
  });
  assert.equal(reviewerFault('ana'), '');
});

const rs = { type: 'requirement', relations: [
  { key: 'out:realized_in', label: 'realized_in', direction: 'out' },
  { key: 'in:adr.affects', label: 'adr.affects', direction: 'in' },
  { key: 'in:use-case.served_by', label: 'serves', direction: 'in' }
] };
const q = list => queryCommand(rs, { q: '', rel: '', attr: [], status: [], trust: [], by: [], not: [], count: [], ...list });

test('a list the CLI can say is copied as its command', () => {
  assert.equal(q({}).command, 'khub query --type requirement');
  assert.equal(q({ attr: ['kind=non-functional'], trust: ['human'], status: ['orphan'], count: ['out:realized_in=0'] }).command,
    "khub query --type requirement --kind non-functional --trust human --orphan --count 'realized_in=0'");
  assert.equal(q({ count: ['in:use-case.served_by>=2'] }).command, "khub query --type requirement --count 'serves>=2'");
  assert.equal(q({ attr: ["owner=it's $(me)"] }).command, "khub query --type requirement --owner 'it'\\''s $(me)'");
});

test('a list it cannot says why', () => {
  [{ q: 'x' }, { rel: 'out:a' }, { by: ['a'] }, { not: ['trust:human'] }, { trust: ['human', 'machine'] }, { status: ['expired'] },
    { count: ['in:adr.affects>=1'] }, { attr: ['kind=a', 'kind=b'] }].forEach(list => {
    assert.equal(q(list).command, '', JSON.stringify(list));
    assert.notEqual(q(list).why, '');
  });
});
