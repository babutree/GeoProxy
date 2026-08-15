'use strict';

const fs = require('fs');
const vm = require('vm');

const bundlePath = process.argv[2];
const scenario = process.argv[3];
if (!bundlePath || !scenario) {
  throw new Error('usage: node dashboard_behavior_harness.js <bundle> <scenario>');
}

const source = fs.readFileSync(bundlePath, 'utf8');
// 契约 wire 载荷：由 Go 测试用真实 handler 采集后落盘（path -> {status, body}）。
const wirePath = process.argv[4];
const wirePayloads = wirePath ? JSON.parse(fs.readFileSync(wirePath, 'utf8')) : null;
const bootMarker = "initFilterToggles();showSkeletons();markViewLazy('overview');";
const bootIndex = source.indexOf(bootMarker);
if (bootIndex < 0) {
  throw new Error('dashboard boot marker not found');
}

function fakeClassList() {
  const values = new Set();
  return {
    add(...names) { names.forEach((name) => values.add(name)); },
    remove(...names) { names.forEach((name) => values.delete(name)); },
    contains(name) { return values.has(name); },
    toggle(name, force) {
      const enabled = force === undefined ? !values.has(name) : Boolean(force);
      if (enabled) values.add(name); else values.delete(name);
      return enabled;
    },
    values,
  };
}

function fakeElement(id) {
  const attributes = new Map();
  return {
    id,
    value: '',
    innerHTML: '',
    textContent: '',
    title: '',
    className: '',
    disabled: false,
    checked: false,
    options: [],
    childNodes: [],
    childElementCount: 0,
    // 轨道几何读 clientWidth/clientHeight；缺省会让 orbitStageGeom 得到 NaN。
    clientWidth: 600,
    clientHeight: 338,
    dataset: {},
    style: { setProperty() {} },
    classList: fakeClassList(),
    setAttribute(name, value) { attributes.set(name, String(value)); },
    getAttribute(name) { return attributes.get(name) || null; },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    appendChild() {},
    remove() {},
  };
}

const elements = new Map();
const ensureElement = (id) => {
  if (!elements.has(id)) elements.set(id, fakeElement(id));
  return elements.get(id);
};
const documentElement = fakeElement('documentElement');
const body = fakeElement('body');
function elementsWithAttribute(name) {
  return Array.from(elements.values()).filter((element) => element.getAttribute(name) !== null);
}
function queryAll(selector) {
  const attribute = /^\[([^\]]+)\]$/.exec(selector);
  if (attribute) return elementsWithAttribute(attribute[1]);
  if (selector === '.filter-toggle[data-sel]') {
    return Array.from(elements.values()).filter((element) => element.classList.contains('filter-toggle') && element.getAttribute('data-sel') !== null);
  }
  if (selector === '#session-rows .session-card' || selector === '.proxy-select' || selector === '.proxy-select:checked') return [];
  return [];
}
const document = {
  body,
  documentElement,
  hidden: false,
  getElementById(id) { return elements.get(id) || null; },
  querySelectorAll(selector) { return queryAll(selector); },
  querySelector(selector) {
    if (selector === '.navitem.active') {
      return Array.from(elements.values()).find((element) => element.classList.contains('navitem') && element.classList.contains('active')) || null;
    }
    return null;
  },
  createElement: fakeElement,
  createElementNS(_namespace, name) { return fakeElement(name); },
};

const clipboardWrites = [];
let confirmResult = true;
let fetchHandler = () => { throw new Error('behavior harness must not perform network requests'); };
const storage = new Map();
const lastClipboardWrite = () => clipboardWrites[clipboardWrites.length - 1];
let rafDepth = 0;
const sandbox = {
  console,
  document,
  location: { hostname: 'localhost', href: '' },
  localStorage: { getItem(key) { return storage.has(key) ? storage.get(key) : null; }, setItem(key, value) { storage.set(key, String(value)); } },
  navigator: {
    clipboard: {
      writeText(value) {
        clipboardWrites.push(String(value));
        return Promise.resolve();
      },
    },
  },
  window: { addEventListener() {} },
  confirm() { return confirmResult; },
  // 同步执行回调，保留生产代码里「双 rAF 等布局」的语义；但轨道动画
  // orbitFrame 会在末尾自我重排，同步递归会炸栈，故限制嵌套深度。
  // 深度耗尽时只返回句柄不执行：ensureOrbitLoop 得到非 0 的 orbitRAF，
  // 不会反复重入，被测的 buildOrbitSvg/buildOrbitSats 仍由场景显式驱动。
  requestAnimationFrame(cb) {
    if (typeof cb !== 'function') return 1;
    if (rafDepth >= 4) return 1;
    rafDepth += 1;
    try { cb(0); } finally { rafDepth -= 1; }
    return 1;
  },
  setTimeout() { return 1; },
  clearTimeout() {},
  setInterval() { return 1; },
  clearInterval() {},
  getComputedStyle() { return { getPropertyValue() { return ''; } }; },
  fetch(...args) { return fetchHandler(...args); },
  btoa(value) { return Buffer.from(value, 'binary').toString('base64'); },
  atob(value) { return Buffer.from(value, 'base64').toString('binary'); },
  Buffer,
  URL,
  encodeURIComponent,
  decodeURIComponent,
  unescape,
};
vm.createContext(sandbox);

// 只去掉启动轮询；函数声明和状态均直接来自生产 dashboardJS。
const exportsScript = `
globalThis.__dashboardBehavior = {
  nodeSupportsInboundProtocol,
  renderProxies,
  aiBadges,
  aiStateOf,
  syncFilterToggle,
  copyProxyCred,
  encodeNodeKeyPin,
  logWindowShift,
  loadLogs,
  loadProxies,
  loadSubscriptions,
  loadSessions,
  showSkeletons,
  deleteSub,
  buildRegionStats,
  buildOrbitSvg,
  buildOrbitSats,
  orbitSessionBeamKey,
  orbitQualityTrack,
  sessionRegionKey,
  subsLoaded() { return subscriptionsLoaded; },
  subs() { return allSubs.slice(); },
  orbitSats() { return orbitSats.map((sat) => ({ q: sat.q, hasBeam: !!sat.beam, live: String(sat.el.className || '').includes('live') })); },
  setOrbitSessions(value) { orbitSessions = value; },
  proxyPagePrev,
  proxyPageNext,
  proxyPageSizeChange,
  applyLang,
  toggleLang,
  lang() { return uiLang; },
  t,
  setCustomStatus(value) { customStatus = value; customStatusLoaded = true; renderCustomStatus(customStatus); },
  setSessions(value) { sessionCache = value; sessionsLoaded = true; renderSessions(sessionCache); },
  setSubscriptions(value) { allSubs = value; subscriptionsLoaded = true; renderSubscriptions(); },
  setAPIKeys(value) { apiKeyCache = value; apiKeysLoaded = true; renderAPIKeys(apiKeyCache); },
  setProxies(value) { allProxies = value; },
  setConfig(value) { configCache = value; },
  setPublicIP(value) { publicIP = value; },
  setPage(value) { proxyPage = value; },
  setPageSize(value) { proxyPageSize = value; },
  page() { return proxyPage; },
  pageSize() { return proxyPageSize; },
  pages() { return proxyTotalPages(); },
  rows() { return proxyRenderRows.slice(); },
  slice() { return proxyPageSlice(); }
};`;
vm.runInContext(source.slice(0, bootIndex) + exportsScript, sandbox, {
  filename: 'dashboard-production.js',
  timeout: 2000,
});
const dashboard = sandbox.__dashboardBehavior;
if (!dashboard) throw new Error('dashboard behavior API was not exported');

function equal(actual, expected, label) {
  if (actual !== expected) {
    throw new Error(`${label}: got ${JSON.stringify(actual)}, want ${JSON.stringify(expected)}`);
  }
}

function equalJSON(actual, expected, label) {
  equal(JSON.stringify(actual), JSON.stringify(expected), label);
}

function runNodeKeyWireScenario() {
  resetDOM();
  const nodeKeys = [
    'ascii-node-key-01',
    'vmess:edge.example.com:443/path?x=1/+=?',
    '节点-日本/東京:443',
    'emoji-😀-é/ß',
  ];
  dashboard.setConfig({
    proxy_auth_username: 'edge',
    proxy_auth_password: 'wire-secret',
    socks5_port: ':7801',
    http_port: ':7802',
  });
  dashboard.setPublicIP('203.0.113.7');
  const wireVectors = [];
  nodeKeys.forEach((nodeKey, index) => {
    const id = 100 + index;
    dashboard.setProxies([proxy(id, {
      address: `127.0.0.1:${12000 + index}`,
      protocol: 'socks5',
      dual_protocol: false,
      node_key: nodeKey,
    })]);
    clipboardWrites.length = 0;
    dashboard.copyProxyCred(id);
    const copiedUsername = decodeURIComponent(new URL(lastClipboardWrite()).username);
    const prefix = 'edge-node-key-';
    equal(copiedUsername.startsWith(prefix), true, `copied username ${index} carries a NodeKey pin`);
    const token = copiedUsername.slice(prefix.length);
    equal(token, dashboard.encodeNodeKeyPin(nodeKey), `copied token ${index} uses the production encoder`);
    wireVectors.push({ nodeKey, token });
  });
  equal(dashboard.encodeNodeKeyPin(''), '', 'empty NodeKey has no wire token');
  equal(dashboard.encodeNodeKeyPin('   '), '', 'whitespace-only NodeKey has no wire token');
  return { scenario: 'nodekey_wire', assertions: wireVectors.length * 2 + 2, wireVectors };
}

const filterIDs = [
  'protocol-filter', 'region-filter', 'status-filter', 'source-filter',
  'quality-filter', 'purity-filter', 'cf-filter', 'ai-openai-filter', 'ai-claude-filter',
  'ai-grok-filter', 'ai-gemini-filter', 'latency-min', 'latency-max',
  'keyword-filter',
];

function resetDOM() {
  filterIDs.forEach((id) => { ensureElement(id).value = ''; });
  [
    'proxy-rows', 'proxy-page-info', 'proxy-page-num', 'proxy-page-prev',
    'proxy-page-next', 'proxy-page-size', 'proxy-select-all', 'toast',
    'confirm-modal', 'confirm-modal-msg', 'confirm-modal-ok', 'confirm-modal-cancel',
    'protocol-pick-modal', 'protocol-pick-socks', 'protocol-pick-http', 'protocol-pick-cancel',
  ].forEach(ensureElement);
  ensureElement('proxy-page-size').value = String(dashboard.pageSize());
}

function i18nElement(id, key, value) {
  const element = ensureElement(id);
  element.setAttribute('data-i18n', key);
  element.textContent = value;
  return element;
}

function setFilter(id, value) {
  ensureElement(id).value = String(value);
}

function proxy(id, overrides) {
  return Object.assign({
    id,
    address: `198.51.100.${id}:8080`,
    protocol: 'http',
    dual_protocol: false,
    region: 'jp',
    status: 'active',
    fail_count: 0,
    source: 'subscription',
    // 与 /api/proxies 合同一致：订阅节点须带 active 父订阅状态才算可选。
    subscription_status: 'active',
    quality_grade: 'A',
    ipapiis_score: 0.05,
    ipapi_flags: '',
    ipapi_flags_seen: true,
    cf_blocked: 0,
    ai_reachability: '{"openai":0,"claude":0,"grok":0,"gemini":0}',
    latency: 100,
    note: `node-${id}`,
  }, overrides || {});
}

function runProtocolScenario() {
  const pureHTTP = proxy(1, { protocol: 'http' });
  const pureSOCKS = proxy(2, { protocol: 'socks5' });
  const dualBool = proxy(3, { protocol: 'socks5', dual_protocol: true });
  const dualNumber = proxy(4, { protocol: 'http', dual_protocol: 1 });
  const cases = [
    [pureHTTP, '', true, 'empty filter accepts HTTP'],
    [pureSOCKS, '  ', true, 'blank filter accepts SOCKS5'],
    [pureHTTP, 'HTTP', true, 'HTTP filter is normalized'],
    [pureHTTP, 'socks5', false, 'pure HTTP rejects SOCKS5'],
    [pureSOCKS, 'socks5', true, 'pure SOCKS5 accepts SOCKS5'],
    [pureSOCKS, 'http', false, 'pure SOCKS5 rejects HTTP'],
    [dualBool, 'http', true, 'dual bool supports HTTP'],
    [dualBool, 'socks5', true, 'dual bool supports SOCKS5'],
    [dualNumber, 'http', true, 'dual numeric supports HTTP'],
    [dualNumber, 'socks5', true, 'dual numeric supports SOCKS5'],
    [dualBool, 'trojan', false, 'dual rejects unknown inbound protocol'],
  ];
  cases.forEach(([node, protocol, want, label]) => {
    equal(dashboard.nodeSupportsInboundProtocol(node, protocol), want, label);
  });
  return { scenario: 'protocol', assertions: cases.length };
}

function runFilterScenario() {
  resetDOM();
  let assertions = 0;
  const assertFilter = (nodes, id, value, want, label) => {
    resetDOM();
    dashboard.setProxies(nodes);
    setFilter(id, value);
    dashboard.renderProxies(false);
    equalJSON(dashboard.rows().map((node) => node.id), want, label);
    assertions += 1;
  };

  assertFilter(
    [proxy(1, { protocol: 'http' }), proxy(2, { protocol: 'socks5' }), proxy(3, { protocol: 'socks5', dual_protocol: true })],
    'protocol-filter', 'http', [1, 3], 'protocol filter uses inbound capabilities',
  );
  assertFilter([proxy(1), proxy(2, { region: 'us' })], 'region-filter', 'jp', [1], 'region filter');
  assertFilter([proxy(1), proxy(2, { user_paused: true })], 'status-filter', 'paused', [2], 'status filter');
  assertFilter([proxy(1), proxy(2, { source: 'manual' })], 'source-filter', 'manual', [2], 'source filter');
  assertFilter([proxy(1), proxy(2, { quality_grade: 'B' })], 'quality-filter', 'A', [1], 'quality-grade filter remains available');

  const purityNodes = [
    proxy(1, { ipapiis_score: 0.05 }),
    proxy(2, { ipapiis_score: 0.25 }),
    proxy(3, { ipapiis_score: 0.75 }),
    proxy(4, { ipapiis_score: -1, ipapi_flags_seen: false }),
    proxy(5, { ipapiis_score: null, ipapi_flags_seen: false }),
  ];
  assertFilter(purityNodes, 'purity-filter', 'clean', [1], 'clean purity filter');
  assertFilter(purityNodes, 'purity-filter', 'caution', [2], 'caution purity filter');
  assertFilter(purityNodes, 'purity-filter', 'risky', [3], 'risky purity filter');
  assertFilter(purityNodes, 'purity-filter', 'unprobed', [4, 5], 'missing and null purity scores remain unprobed');

  const cfNodes = [proxy(1, { cf_blocked: 0 }), proxy(2, { cf_blocked: 1 }), proxy(3, { cf_blocked: -1 })];
  assertFilter(cfNodes, 'cf-filter', 'unlocked', [1], 'Cloudflare unlocked filter');
  assertFilter(cfNodes, 'cf-filter', 'blocked', [2], 'Cloudflare blocked filter');
  assertFilter(cfNodes, 'cf-filter', 'unknown', [3], 'Cloudflare unknown filter');

  ['openai', 'claude', 'grok', 'gemini'].forEach((service, serviceIndex) => {
    const filterID = `ai-${service}-filter`;
    const reachability = (value) => JSON.stringify({ [service]: value });
    const nodes = [
      proxy(serviceIndex * 10 + 1, { ai_reachability: reachability(0) }),
      proxy(serviceIndex * 10 + 2, { ai_reachability: reachability(1) }),
      proxy(serviceIndex * 10 + 3, { ai_reachability: '{}' }),
    ];
    assertFilter(nodes, filterID, 'unlocked', [serviceIndex * 10 + 1], `${service} unlocked filter`);
    assertFilter(nodes, filterID, 'blocked', [serviceIndex * 10 + 2], `${service} blocked filter`);
    assertFilter(nodes, filterID, 'unprobed', [serviceIndex * 10 + 3], `${service} unprobed filter`);
  });

  const latencyNodes = [proxy(1, { latency: 80 }), proxy(2, { latency: 160 }), proxy(3, { latency: 320 }), proxy(4, { latency: 0 })];
  assertFilter(latencyNodes, 'latency-min', '100', [2, 3], 'minimum latency filter excludes unknown latency');
  assertFilter(latencyNodes, 'latency-max', '200', [1, 2], 'maximum latency filter excludes unknown latency');
  resetDOM();
  dashboard.setProxies(latencyNodes);
  setFilter('latency-min', '100');
  setFilter('latency-max', '200');
  dashboard.renderProxies(false);
  equalJSON(dashboard.rows().map((node) => node.id), [2], 'latency interval composes');
  assertions += 1;

  const keywordNodes = [
    proxy(1, { address: 'edge.example:443' }),
    proxy(2, { note: 'Tokyo Needle' }),
    proxy(3, { exit_ip: '203.0.113.9' }),
  ];
  assertFilter(keywordNodes, 'keyword-filter', 'EDGE.EXAMPLE', [1], 'address keyword is case-insensitive');
  assertFilter(keywordNodes, 'keyword-filter', 'tokyo needle', [2], 'note keyword');
  assertFilter(keywordNodes, 'keyword-filter', '203.0.113.9', [3], 'exit IP keyword');

  const target = proxy(101, {
    protocol: 'socks5', dual_protocol: 1, region: ' JP ', status: 'degraded',
    source: 'subscription', quality_grade: 'A', ipapiis_score: 0.05, cf_blocked: 0,
    ai_reachability: '{"openai":0,"claude":0,"grok":0,"gemini":0}',
    latency: 120, note: 'Needle target',
  });
  const decoys = [
    proxy(102, { protocol: 'socks5', dual_protocol: false, note: 'Needle target' }),
    proxy(103, { protocol: 'socks5', dual_protocol: true, region: 'us', note: 'Needle target' }),
    proxy(104, { protocol: 'socks5', dual_protocol: true, user_paused: true, note: 'Needle target' }),
    proxy(105, { protocol: 'socks5', dual_protocol: true, source: 'manual', note: 'Needle target' }),
    proxy(106, { protocol: 'socks5', dual_protocol: true, quality_grade: 'B', note: 'Needle target' }),
    proxy(107, { protocol: 'socks5', dual_protocol: true, ipapiis_score: 0.8, note: 'Needle target' }),
    proxy(108, { protocol: 'socks5', dual_protocol: true, cf_blocked: 1, note: 'Needle target' }),
    proxy(109, { protocol: 'socks5', dual_protocol: true, ai_reachability: '{"openai":1,"claude":0,"grok":0,"gemini":0}', note: 'Needle target' }),
    proxy(110, { protocol: 'socks5', dual_protocol: true, latency: 300, note: 'Needle target' }),
    proxy(111, { protocol: 'socks5', dual_protocol: true, note: 'different keyword' }),
  ];
  resetDOM();
  dashboard.setProxies([target, ...decoys]);
  [
    ['protocol-filter', 'http'], ['region-filter', 'jp'], ['status-filter', 'ok'],
    ['source-filter', 'subscription'], ['quality-filter', 'A'], ['purity-filter', 'clean'],
    ['cf-filter', 'unlocked'], ['ai-openai-filter', 'unlocked'], ['ai-claude-filter', 'unlocked'],
    ['ai-grok-filter', 'unlocked'], ['ai-gemini-filter', 'unlocked'],
    ['latency-min', '100'], ['latency-max', '200'], ['keyword-filter', 'needle target'],
  ].forEach(([id, value]) => setFilter(id, value));
  dashboard.renderProxies(false);
  equalJSON(dashboard.rows().map((node) => node.id), [101], 'all advanced and legacy filters compose');
  assertions += 1;

  setFilter('keyword-filter', 'no-such-node');
  dashboard.renderProxies(false);
  equalJSON(dashboard.rows(), [], 'empty filter result has no rows');
  equal(ensureElement('proxy-rows').innerHTML.includes('没有匹配节点'), true, 'empty result explains that no node matched');
  assertions += 2;
  return { scenario: 'filters', assertions };
}

function countText(value, needle) {
  return String(value).split(needle).length - 1;
}

function runAIBadgeScenario() {
  const rendered = dashboard.aiBadges('{"openai":0,"claude":1,"grok":-1,"gemini":0}');
  [['GPT', 'ChatGPT'], ['Cld', 'Claude'], ['Grk', 'Grok'], ['Gem', 'Gemini']].forEach(([label, service]) => {
    equal(countText(rendered, `<span class="nm">${label}</span>`), 1, `${service} keeps the published short label`);
  });
  equal(rendered.includes('title="ChatGPT 畅通"'), true, 'ChatGPT reachable title');
  equal(rendered.includes('title="Claude 阻断"'), true, 'Claude blocked title');
  equal(rendered.includes('title="Grok 未探测"'), true, 'Grok unprobed title');
  equal(rendered.includes('title="Gemini 畅通"'), true, 'Gemini reachable title');
  equal(rendered.includes('role="img"'), true, 'AI badges expose image semantics');
  equal(rendered.includes('aria-label="Claude 阻断"'), true, 'AI badge state is available to assistive technology');
  equal(rendered.includes('<img'), false, 'AI icons do not load image resources');
  equal(rendered.includes('http://') || rendered.includes('https://'), false, 'AI icons have no external URL');

  const malformed = dashboard.aiBadges('{bad json');
  equal(malformed.includes('class="muted"'), true, 'bad JSON renders the published unavailable marker');
  equal(countText(malformed, 'class="ai-mark'), 0, 'bad JSON does not fabricate per-service states');
  const missing = dashboard.aiBadges('{"openai":0}');
  equal(countText(missing, 'class="ai-mark na"'), 3, 'missing fields remain unprobed');
  equal(dashboard.aiStateOf({ ai_reachability: '{"openai":null}' }, 'openai'), 'unprobed', 'null is not reachable');
  equal(dashboard.aiStateOf({ ai_reachability: '{"openai":false}' }, 'openai'), 'unprobed', 'boolean false is not reachable');
  equal(dashboard.aiStateOf({ ai_reachability: '{bad json' }, 'openai'), 'unprobed', 'bad JSON is unprobed');
  return { scenario: 'ai_badges', assertions: 18 };
}

function runFilterToggleScenario() {
  const select = ensureElement('cf-filter');
  const button = ensureElement('cf-toggle');
  const cases = [
    ['', 'all', 'false'],
    ['unlocked', 'ok', 'true'],
    ['blocked', 'bad', 'true'],
    ['unknown', 'unk', 'true'],
  ];
  cases.forEach(([value, state, pressed]) => {
    select.value = value;
    dashboard.syncFilterToggle('cf-filter', 'cf-toggle');
    equal(button.dataset.state, state, `${value || 'all'} visual state`);
    equal(button.getAttribute('aria-pressed'), pressed, `${value || 'all'} pressed state`);
  });
  return { scenario: 'filter_toggle', assertions: cases.length * 2 };
}

function runPaginationScenario() {
  resetDOM();
  dashboard.setPageSize(20);
  ensureElement('proxy-page-size').value = '20';
  const nodes = Array.from({ length: 45 }, (_, index) => proxy(index + 1, {
    region: index < 2 ? 'jp' : 'us',
    latency: index + 1,
  }));
  dashboard.setProxies(nodes);
  dashboard.renderProxies(false);
  equal(dashboard.page(), 1, 'initial page');
  equal(dashboard.pages(), 3, '45 rows have three pages');
  equal(dashboard.slice().length, 20, 'first page size');

  dashboard.proxyPageNext();
  equal(dashboard.page(), 2, 'next enters second page');
  equal(dashboard.slice().length, 20, 'second page size');
  dashboard.proxyPageNext();
  equal(dashboard.page(), 3, 'next enters last page');
  equal(dashboard.slice().length, 5, 'last page remainder');
  dashboard.proxyPageNext();
  equal(dashboard.page(), 3, 'next is bounded at last page');

  dashboard.proxyPagePrev();
  dashboard.proxyPagePrev();
  dashboard.proxyPagePrev();
  equal(dashboard.page(), 1, 'previous is bounded at first page');

  dashboard.proxyPageNext();
  dashboard.proxyPageNext();
  setFilter('region-filter', 'jp');
  dashboard.renderProxies(false);
  equal(dashboard.page(), 1, 'filter change resets page');
  equal(dashboard.rows().length, 2, 'filter change updates result count');

  setFilter('region-filter', '');
  dashboard.setProxies(nodes);
  dashboard.renderProxies(false);
  dashboard.proxyPageNext();
  dashboard.proxyPageNext();
  dashboard.setProxies(nodes.slice(0, 21));
  dashboard.renderProxies(true);
  equal(dashboard.page(), 2, 'refresh clamps page after result shrink');
  equal(dashboard.pages(), 2, 'shrunk results have two pages');
  equal(dashboard.slice().length, 1, 'clamped last page has remainder');
  return { scenario: 'pagination', assertions: 13 };
}

function runLanguageScenario() {
  resetDOM();
  storage.clear();
  const nav = i18nElement('i18n-nav-overview', 'nav_overview', '总览');
  const button = i18nElement('i18n-copy', 'btn_copy', '复制');
  const title = ensureElement('pageTitle');
  const langCode = ensureElement('lang-code');
  const activeNav = ensureElement('i18n-active-nav');
  activeNav.classList.add('navitem', 'active');
  activeNav.dataset.tab = 'overview';
  dashboard.setProxies([proxy(1, { user_paused: true })]);
  dashboard.renderProxies(false);
  ['singbox-status', 'session-rows', 'sess-count', 'ov-session-rows', 'ov-sess-count', 'sub-list', 'apikey-rows'].forEach(ensureElement);
  dashboard.setCustomStatus({ singbox_status: 'running', singbox_nodes: 2, singbox_ready_ports: 2, singbox_total_ports: 2, subscription_count: 1, disabled_count: 0, subscription_total: 1 });
  dashboard.setSessions([{
    session_id: 'session-1', region: 'jp', source: 'manual', protocol: 'http',
    quality_grade: 'A', latency: 100, remaining_ttl_seconds: 120,
  }]);
  dashboard.setSubscriptions([{ id: 1, name: 'main', status: 'active', active_count: 1, disabled_count: 0, proxy_count: 1 }]);
  dashboard.setAPIKeys([{ id: 'key-1', name: 'readonly', created_at: '2026-01-02T03:04:05Z', disabled: true }]);

  dashboard.applyLang('en');
  equal(dashboard.lang(), 'en', 'explicit English language selection');
  equal(storage.get('gg-lang'), 'en', 'English selection persists');
  equal(documentElement.lang, 'en', 'document language is English');
  equal(documentElement.getAttribute('data-lang'), 'en', 'document data language is English');
  equal(nav.textContent, 'Overview', 'static navigation label is translated');
  equal(button.textContent, 'Copy', 'static action label is translated');
  equal(title.textContent, 'Overview', 'active page title is translated');
  equal(langCode.textContent, '中文', 'toggle indicates Chinese target after English selection');
  equal(ensureElement('proxy-rows').innerHTML.includes('Enable'), true, 'cached proxy rows are immediately re-rendered in English');
  equal(ensureElement('singbox-status').innerHTML.includes('Running'), true, 'cached sing-box status is immediately re-rendered in English');
  equal(ensureElement('session-rows').innerHTML.includes('Session ID'), true, 'cached sessions are immediately re-rendered in English');
  equal(ensureElement('sub-list').innerHTML.includes('Edit'), true, 'cached subscriptions are immediately re-rendered in English');
  equal(ensureElement('apikey-rows').innerHTML.includes('Revoked'), true, 'cached API keys are immediately re-rendered in English');

  dashboard.toggleLang();
  equal(dashboard.lang(), 'zh', 'toggle returns to Chinese');
  equal(storage.get('gg-lang'), 'zh', 'Chinese selection persists');
  equal(documentElement.lang, 'zh-CN', 'document language returns to Chinese');
  equal(nav.textContent, '总览', 'static navigation label returns to Chinese');
  equal(button.textContent, '复制', 'static action label returns to Chinese');
  equal(title.textContent, '总览', 'active page title returns to Chinese');
  equal(langCode.textContent, 'EN', 'toggle indicates English target after Chinese selection');
  equal(ensureElement('proxy-rows').innerHTML.includes('启用'), true, 'cached proxy rows are immediately re-rendered in Chinese');
  equal(ensureElement('singbox-status').innerHTML.includes('运行中'), true, 'cached sing-box status is immediately re-rendered in Chinese');
  equal(ensureElement('session-rows').innerHTML.includes('会话 ID'), true, 'cached sessions are immediately re-rendered in Chinese');
  equal(ensureElement('sub-list').innerHTML.includes('修改'), true, 'cached subscriptions are immediately re-rendered in Chinese');
  equal(ensureElement('apikey-rows').innerHTML.includes('已吊销'), true, 'cached API keys are immediately re-rendered in Chinese');
  return { scenario: 'language', assertions: 24 };
}

function createLogBox(lines, scrollTop, boxTop = 100, lineHeight = 20) {
  const box = fakeElement('logs-box');
  let children = [];
  let markup = '';
  box.scrollTop = scrollTop;
  box.getBoundingClientRect = () => ({ top: boxTop, bottom: boxTop + 80 });
  Object.defineProperty(box, 'children', { get() { return children; } });
  Object.defineProperty(box, 'innerHTML', {
    get() { return markup; },
    set(value) {
      markup = String(value);
      children = Array.from(markup.matchAll(/<div class="log-line">([^<]*)<\/div>/g), (match, index) => ({
        textContent: match[1],
        getBoundingClientRect() {
          const top = boxTop + index * lineHeight - box.scrollTop;
          return { top, bottom: top + lineHeight };
        },
      }));
    },
  });
  box.innerHTML = lines.map((line) => `<div class="log-line">${line}</div>`).join('');
  return box;
}

async function runLogWindowScenario() {
  equal(dashboard.logWindowShift(['a', 'b'], ['a', 'b']), 0, 'unchanged window has no shift');
  equal(dashboard.logWindowShift(['a', 'b'], ['a', 'b', 'c']), 0, 'append-only window keeps indices');
  equal(dashboard.logWindowShift(['dup', 'dup', 'tail'], ['dup', 'tail', 'new']), 1, 'largest overlap disambiguates duplicate text');
  equal(dashboard.logWindowShift(['a', 'b', 'c'], ['c', 'd', 'e']), 2, 'rotated window maps the surviving suffix');
  equal(dashboard.logWindowShift(['a'], ['z']), null, 'unrelated windows have no anchor mapping');
  const page = ensureElement('page-logs');
  page.classList.add('active');
  const auto = ensureElement('logs-autoscroll');
  auto.checked = false;
  const box = createLogBox(['prefix', 'dup', 'dup', 'tail'], 45);
  elements.set('logs-box', box);
  let requestedPath = '';
  fetchHandler = async (path) => {
    requestedPath = path;
    return {
      status: 200,
      ok: true,
      statusText: 'OK',
      async text() { return JSON.stringify({ lines: ['dup', 'tail', 'new'] }); },
    };
  };
  await dashboard.loadLogs();
  equal(requestedPath, '/api/logs', 'scenario executes production loadLogs request path');
  equal(box.children[0].textContent, 'dup', 'head-trimmed duplicate anchor maps to surviving row');
  equal(box.children[0].getBoundingClientRect().top, 95, 'visible anchor keeps its viewport coordinate');
  equal(box.scrollTop, 5, 'unchecked auto-scroll restores anchor instead of jumping');
  return { scenario: 'log_window', assertions: 9 };
}

async function runSessionScenario() {
  resetDOM();
  ['session-rows', 'sess-count', 'ov-session-rows', 'ov-sess-count'].forEach(ensureElement);
  dashboard.applyLang('en');
  dashboard.setSessions([{
    session_id: 'gb-request-us-exit',
    selected_region: 'gb',
    exit_ip: '81.90.21.44',
    exit_region: 'us',
    exit_location: 'US Seattle',
    exit_checked_at: '2026-08-10T02:36:00Z',
    bind_address: '127.0.0.1:31001',
    protocol: 'socks5',
    source: 'manual',
    remaining_ttl_seconds: 120,
  }]);
  const card = ensureElement('session-rows').innerHTML;
  equal(card.includes('81.90.21.44'), true, 'session card shows verified exit IP');
  equal(card.includes('US'), true, 'session card shows verified exit region');
  equal(card.includes('US Seattle'), true, 'session card shows verified exit location');
  equal(card.includes('2026-08-10T02:36:00Z'), true, 'session card shows exit snapshot time');
  equal(card.includes('region-gb'), true, 'session card keeps selected routing region separately');
  equal(card.includes('>GB<'), false, 'selected routing region is not rendered as exit region');
  equal(card.includes('127.0.0.1:31001'), true, 'session card keeps local bind separate from exit');
  return { scenario: 'session', assertions: 7 };
}

async function runEmptyLogsI18NScenario() {
  resetDOM();
  const page = ensureElement('page-logs');
  page.classList.add('active');
  const auto = ensureElement('logs-autoscroll');
  auto.checked = false;
  const box = createLogBox([], 0);
  elements.set('logs-box', box);
  fetchHandler = async () => ({
    status: 200,
    ok: true,
    statusText: 'OK',
    async text() { return JSON.stringify({ lines: [] }); },
  });
  dashboard.applyLang('zh');
  await dashboard.loadLogs();
  equal(box.children[0].textContent, '暂无日志', 'empty logs use Chinese translation');
  dashboard.applyLang('en');
  await dashboard.loadLogs();
  equal(box.children[0].textContent, 'No logs', 'empty logs use English translation');
  return { scenario: 'logs_empty', assertions: 2 };
}

async function runCopyScenario() {
  resetDOM();
  const nodeKey = 'vmess:东京-session-x.example:443:abc/+=?';
  const password = "p@ss:/?#[]!'()*";
  const gateway = proxy(1, {
    address: '127.0.0.1:1080',
    protocol: 'socks5',
    dual_protocol: true,
    node_key: nodeKey,
  });
  const direct = proxy(2, {
    address: '198.51.100.8:8080',
    protocol: 'http',
    dual_protocol: false,
    username: 'up stream',
    password: 'direct@secret',
  });
  const legacyGateway = proxy(3, {
    address: '127.0.0.1:1099',
    protocol: 'socks5',
    dual_protocol: true,
    node_key: '',
  });
  dashboard.setProxies([gateway, direct, legacyGateway]);
  dashboard.setConfig({
    proxy_auth_username: 'edge',
    proxy_auth_password: password,
    socks5_port: ':7801',
    http_port: ':7802',
  });
  dashboard.setPublicIP('203.0.113.7');

  // mixed 节点通过应用内协议选择，不再用浏览器 confirm 的确定/取消冒充协议。
  const originalPick = sandbox.showProtocolPick;
  sandbox.showProtocolPick = async function () { return 'socks5'; };
  clipboardWrites.length = 0;
  await dashboard.copyProxyCred(1);
  const socksURLRaw = lastClipboardWrite();
  const socksURL = new URL(socksURLRaw);
  const expectedPin = Buffer.from(nodeKey, 'utf8').toString('base64')
    .replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  const gatewayUser = decodeURIComponent(socksURL.username);
  equal(socksURL.protocol, 'socks5:', 'protocol pick socks5 copies SOCKS5 URL');
  equal(socksURL.hostname, '203.0.113.7', 'gateway copy uses public host');
  equal(socksURL.port, '7801', 'SOCKS5 copy uses configured gateway port');
  equal(gatewayUser, `edge-node-key-${expectedPin}`, 'gateway copy uses stable Base64URL node key');
  equal(decodeURIComponent(socksURL.password), password, 'gateway password survives userinfo escaping');
  equal(/[+/=]/.test(expectedPin), false, 'node key pin is unpadded Base64URL');
  equal(socksURLRaw.includes(password), false, 'raw URL does not contain unescaped password');

  sandbox.showProtocolPick = async function () { return 'http'; };
  await dashboard.copyProxyCred(1);
  const httpURL = new URL(lastClipboardWrite());
  equal(httpURL.protocol, 'http:', 'protocol pick http copies HTTP URL');
  equal(httpURL.port, '7802', 'HTTP copy uses configured gateway port');

  const writesBeforeCancel = clipboardWrites.length;
  sandbox.showProtocolPick = async function () { return ''; };
  await dashboard.copyProxyCred(1);
  equal(clipboardWrites.length, writesBeforeCancel, 'protocol pick cancel does not write clipboard');
  sandbox.showProtocolPick = originalPick;

  dashboard.copyProxyCred(2);
  await Promise.resolve();
  const directURL = new URL(lastClipboardWrite());
  equal(directURL.protocol, 'http:', 'direct copy preserves node protocol');
  equal(directURL.host, '198.51.100.8:8080', 'direct copy preserves node endpoint');
  equal(decodeURIComponent(directURL.username), 'up stream', 'direct username is escaped');
  equal(decodeURIComponent(directURL.password), 'direct@secret', 'direct password is escaped');

  dashboard.setConfig({
    proxy_auth_username: 'edge',
    proxy_auth_password: '',
    socks5_port: ':7801',
    http_port: ':7802',
  });
  sandbox.showProtocolPick = async function () { return 'socks5'; };
  await dashboard.copyProxyCred(1);
  const placeholderURL = new URL(lastClipboardWrite());
  equal(decodeURIComponent(placeholderURL.password), 'PASSWORD', 'missing password uses explicit placeholder');
  equal(ensureElement('toast').textContent.includes(password), false, 'toast does not expose gateway password');

  const writesBeforeLegacyCopy = clipboardWrites.length;
  await dashboard.copyProxyCred(3);
  equal(clipboardWrites.length, writesBeforeLegacyCopy, 'gateway without NodeKey does not write an unstable address pin');
  equal(
    ensureElement('toast').textContent,
    '无法复制：该网关节点缺少稳定 NodeKey，请刷新订阅或重新导入节点后重试',
    'gateway without NodeKey explains the stable identity migration',
  );
  equal(
    clipboardWrites.some((value) => value.includes('edge-node-127.0.0.1:1099')),
    false,
    'clipboard never receives the temporary loopback address pin',
  );
  return { scenario: 'copy', assertions: 19, gatewayUser, nodeKey };
}

// wireResponse 把 Go 侧真实 handler 采集的 {status, body} 包装成 fetch 响应。
// 这里绝不手写 JSON 字面量：整个契约场景的输入必须来自真实 handler，
// 否则字段改名 / nil 切片这类后端契约漂移仍然测不出来。
function wireResponse(path) {
  if (!wirePayloads) throw new Error('wire payload file is required for contract scenarios');
  const payload = wirePayloads[path];
  if (!payload) throw new Error(`wire payload missing for ${path}`);
  return {
    status: payload.status,
    ok: payload.status >= 200 && payload.status < 300,
    statusText: 'OK',
    async text() { return payload.body; },
  };
}

// 契约场景 1：空订阅表时的真实响应必须让前端把列表清空并结束骨架态。
// 覆盖「删掉最后一条订阅后列表仍显示旧订阅」以及「刷新后订阅页卡在骨架」。
async function runSubscriptionEmptyWireScenario() {
  resetDOM();
  ['sub-list', 'toast'].forEach(ensureElement);
  fetchHandler = async (path) => wireResponse(String(path));

  const box = ensureElement('sub-list');
  dashboard.showSkeletons();
  equal(box.innerHTML.includes('skeleton'), true, 'boot renders subscription skeletons');

  await dashboard.loadSubscriptions();
  equal(dashboard.subsLoaded(), true, 'empty subscription wire response completes the load');
  equal(dashboard.subs().length, 0, 'empty subscription wire response clears cached rows');
  equal(box.innerHTML.includes('skeleton'), false, 'empty subscription wire response replaces skeletons');
  equal(box.innerHTML.includes('暂无订阅'), true, 'empty subscription wire response renders the empty state');
  return { scenario: 'subs_empty_wire', assertions: 5 };
}

// 契约场景 2：删除最后一条订阅后，列表必须真正消失（不依赖手写 mock 的 []）。
async function runSubscriptionDeleteLastScenario() {
  resetDOM();
  ['sub-list', 'toast', 'confirm-modal', 'confirm-modal-msg', 'confirm-modal-ok', 'confirm-modal-cancel',
    // deleteSub 成功后并发刷新 stats/proxies/subscriptions，这些节点必须存在，
    // 否则真实代码会抛 TypeError 并被 runAsync 吞成 toast，掩盖删除结果。
    'stat-total', 'stat-http', 'stat-socks5', 'stat-subscription', 'stat-sessions',
    'region-filter', 'proxy-rows', 'region-list', 'region-page-list', 'region-total', 'region-page-total',
    'orbit-stage', 'orbit-svg', 'orbit-sats', 'orbit-beams', 'orbit-gw-ip',
    'proxy-page-info', 'proxy-page-num', 'proxy-page-prev', 'proxy-page-next', 'proxy-page-size'].forEach(ensureElement);
  const box = ensureElement('sub-list');
  const okBtn = ensureElement('confirm-modal-ok');

  // 起始状态用「删除前」的真实 handler 响应渲染。
  fetchHandler = async () => wireResponse('/api/subscriptions#before');
  await dashboard.loadSubscriptions();
  equal(dashboard.subs().length, 1, 'pre-delete wire response lists one subscription');
  equal(box.innerHTML.includes('wire-sub'), true, 'pre-delete list renders the subscription name');

  // 删除后列表端点返回「删除后」的真实响应（空表）。
  fetchHandler = async (path) => wireResponse(String(path) === '/api/subscriptions' ? '/api/subscriptions#after' : String(path));
  const pending = dashboard.deleteSub(1);
  // showConfirm 把确认按钮的 onclick 挂在 DOM 上；模拟用户点「确定」。
  await Promise.resolve();
  equal(typeof okBtn.onclick, 'function', 'delete flow opens the in-app confirm dialog');
  okBtn.onclick();
  await pending;

  equal(dashboard.subs().length, 0, 'confirmed delete drops the subscription from cache');
  equal(box.innerHTML.includes('wire-sub'), false, 'confirmed delete removes the row from the DOM');
  equal(ensureElement('toast').textContent, '订阅已删除', 'confirmed delete reports success');
  return { scenario: 'subs_delete_last', assertions: 6 };
}

// 契约场景 3：会话连线。用真实 /api/sessions + /api/proxies 响应驱动轨道，
// 断言「有 sticky 会话 ⇒ 至少一条连线 + 卫星点亮」。
// 这是字段改名（region → selected_region）回归的直接拦截点。
async function runOrbitSessionBeamWireScenario() {
  resetDOM();
  ['orbit-stage', 'orbit-svg', 'orbit-sats', 'orbit-beams', 'orbit-gw-ip',
    'session-rows', 'sess-count', 'ov-session-rows', 'ov-sess-count',
    'region-list', 'region-page-list', 'region-total', 'region-page-total',
    'proxy-rows', 'region-filter'].forEach(ensureElement);
  fetchHandler = async (path) => wireResponse(String(path));

  await dashboard.loadProxies();
  await dashboard.loadSessions();

  const sessions = JSON.parse(wirePayloads['/api/sessions'].body);
  equal(Array.isArray(sessions) && sessions.length > 0, true, 'wire payload carries at least one sticky binding');
  equal('region' in sessions[0], false, 'wire contract no longer exposes the legacy region field');

  // 轨道必须被真实渲染路径构建（loadSessions → renderSessions → renderOrbitSystem）。
  dashboard.buildOrbitSvg();
  dashboard.buildOrbitSats();
  const sats = dashboard.orbitSats();
  equal(sats.length > 0, true, 'available nodes produce orbit satellites');
  equal(sats.filter((sat) => sat.hasBeam).length > 0, true, 'sticky sessions produce at least one session beam');
  equal(sats.filter((sat) => sat.live).length > 0, true, 'satellites bound to sessions are marked live');

  // 地域面板的会话计数同样来自 sessionRegionKey，必须与连线一致。
  const stats = dashboard.buildRegionStats();
  equal(stats.reduce((sum, item) => sum + Number(item.sess || 0), 0) > 0, true, 'region panel counts sticky sessions');

  // 键必须落在真实存在的地域桶上，不能是空串。
  equal(dashboard.sessionRegionKey(sessions[0]) !== '', true, 'session region key resolves from the wire contract');
  return { scenario: 'orbit_session_beams', assertions: 7 };
}

// 契约场景 4：删除接口返回 500「订阅已删除但文件清理失败」时，
// 服务端状态已变，前端必须仍然刷新列表并把错误报给用户。
async function runSubscriptionDeletePartialFailureScenario() {
  resetDOM();
  ['sub-list', 'toast', 'confirm-modal', 'confirm-modal-msg', 'confirm-modal-ok', 'confirm-modal-cancel',
    'stat-total', 'stat-http', 'stat-socks5', 'stat-subscription', 'stat-sessions',
    'region-filter', 'proxy-rows', 'region-list', 'region-page-list', 'region-total', 'region-page-total',
    'orbit-stage', 'orbit-svg', 'orbit-sats', 'orbit-beams', 'orbit-gw-ip',
    'proxy-page-info', 'proxy-page-num', 'proxy-page-prev', 'proxy-page-next', 'proxy-page-size'].forEach(ensureElement);
  const box = ensureElement('sub-list');
  const okBtn = ensureElement('confirm-modal-ok');

  fetchHandler = async () => wireResponse('/api/subscriptions#before');
  await dashboard.loadSubscriptions();
  equal(dashboard.subs().length, 1, 'pre-delete wire response lists one subscription');

  fetchHandler = async (path) => wireResponse(String(path) === '/api/subscriptions' ? '/api/subscriptions#after' : String(path));
  const pending = dashboard.deleteSub(1);
  await Promise.resolve();
  equal(typeof okBtn.onclick, 'function', 'delete flow opens the in-app confirm dialog');
  okBtn.onclick();
  await pending;

  equal(dashboard.subs().length, 0, 'partial delete failure still refreshes the cached list');
  equal(box.innerHTML.includes('wire-sub'), false, 'partial delete failure still removes the stale row');
  equal(
    ensureElement('toast').textContent.includes('file cleanup failed'),
    true,
    'partial delete failure surfaces the server error instead of a success toast',
  );
  return { scenario: 'subs_delete_partial_failure', assertions: 5 };
}

const scenarios = {
  protocol: runProtocolScenario,
  filters: runFilterScenario,
  filter_toggle: runFilterToggleScenario,
  ai_badges: runAIBadgeScenario,
  pagination: runPaginationScenario,
  language: runLanguageScenario,
  copy: runCopyScenario,
  nodekey_wire: runNodeKeyWireScenario,
  log_window: runLogWindowScenario,
  session: runSessionScenario,
  logs_empty: runEmptyLogsI18NScenario,
  subs_empty_wire: runSubscriptionEmptyWireScenario,
  subs_delete_last: runSubscriptionDeleteLastScenario,
  subs_delete_partial_failure: runSubscriptionDeletePartialFailureScenario,
  orbit_session_beams: runOrbitSessionBeamWireScenario,
};

Promise.resolve(scenarios[scenario] ? scenarios[scenario]() : Promise.reject(new Error(`unknown scenario ${scenario}`)))
  .then((result) => process.stdout.write(`${JSON.stringify(result)}\n`))
  .catch((error) => {
    process.stderr.write(`DASHBOARD_BEHAVIOR_FAIL ${scenario}: ${error.stack || error}\n`);
    process.exitCode = 1;
  });
