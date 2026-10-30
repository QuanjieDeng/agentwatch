import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DICT, resolveLang, tr } from './i18n.js';

test('语言优先级：URL > 上次选择 > 浏览器 > 中文', () => {
  assert.equal(resolveLang('en', 'zh', 'zh-CN'), 'en');
  assert.equal(resolveLang('zh', 'en', 'en-US'), 'zh');
  assert.equal(resolveLang('fr', 'en', 'zh-CN'), 'en');
  assert.equal(resolveLang(null, null, 'en-US'), 'en');
  assert.equal(resolveLang(null, null, 'zh-CN'), 'zh');
  assert.equal(resolveLang(null, null, ''), 'zh');
});

test('中英文案键一致，默认中文', () => {
  assert.deepEqual(Object.keys(DICT.en).sort(), Object.keys(DICT.zh).sort());
  assert.equal(tr('rooms'), '房间');
  assert.equal(tr('agentCount', 2), '2 个');
});
