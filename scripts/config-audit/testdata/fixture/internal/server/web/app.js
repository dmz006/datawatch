const GENERAL_CONFIG_FIELDS = [
  { id: 'general', section: 'General', fields: [
    { key: 'hostname', label: 'Host', type: 'text' },
    { key: 'no_ref', label: 'No ref', type: 'text' },
    { key: 'no_get', label: 'No get', type: 'number' },
    { key: 'no_put', label: 'No put', type: 'number' },
    { key: '_cert_source', label: 'Internal row', type: 'html' },
    { key: 'untagged', label: 'Untagged', type: 'text' },
    { key: "server.port", label: 'Port', type: 'number' },
  ] },
];
const LLM_CONFIG_FIELDS = [
  { id: 'llm', fields: [
    { key: 'server.limits.max', label: 'Max', type: 'number' },
    { key: 'list', label: 'List', type: 'list' },
    { key: 'labels', label: 'Labels', type: 'map' },
    { key: 'wait', label: 'Wait', type: 'number' },
  ] },
];
// A row defined outside the three field arrays still counts.
const EXTRA_ROWS = [
  { key: 'server.tls', label: 'TLS', type: 'toggle' },
  { key: 'not.a.config.key', label: 'Decoy that is not a config key', type: 'text' },
];
// Hand-built controls count too: a save call naming the key, or a data-cfg-key attribute.
const HAND_BUILT = `
  <input class="x" onchange="saveGeneralField('shared_one', this.value)" />
  <input type="checkbox" data-cfg-key="ptr.flag" />
  <button onclick="shareThing('no_web.decoy', 1)">not a save call</button>
`;
function saveEmbedded(v) { return fetch('/api/config', { method: 'PUT', body: JSON.stringify({ 'embedded.embedded_one': v }) }); }
