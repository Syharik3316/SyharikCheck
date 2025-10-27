const API_BASE = process.env.REACT_APP_API_BASE || (typeof window !== 'undefined' ? window.location.origin : 'http://localhost:8080');

export async function createCheck(target, methods) {
  const resp = await fetch(`${API_BASE}/api/check`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ target, methods })
  });
  if (!resp.ok) throw new Error('Failed to create check');
  return resp.json();
}

export async function getCheck(id) {
  const resp = await fetch(`${API_BASE}/api/check/${id}`);
  if (!resp.ok) throw new Error('Failed to get check');
  return resp.json();
}

export function openResultsWS(onMessage) {
  const wsUrl = API_BASE.replace(/^http(s?):/, (m, s) => (s ? 'wss:' : 'ws:')) + '/api/ws';
  const ws = new WebSocket(wsUrl);
  ws.onmessage = (e) => {
    try { const data = JSON.parse(e.data); onMessage?.(data); } catch (_) {}
  };
  return ws;
}

export async function adminListAgentsBasic(user, pass) {
  const resp = await fetch(`${API_BASE}/api/admin/agents`, {
    headers: { 'Authorization': 'Basic ' + btoa(`${user}:${pass}`) }
  });
  if (!resp.ok) throw new Error('Failed to list agents');
  return resp.json();
}

export async function adminCreateAgentBasic(user, pass, name, region) {
  const resp = await fetch(`${API_BASE}/api/admin/agents`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Authorization': 'Basic ' + btoa(`${user}:${pass}`) },
    body: JSON.stringify({ name, region })
  });
  if (!resp.ok) throw new Error('Failed to create agent');
  return resp.json();
}

export async function adminDeleteAgentBasic(user, pass, id) {
  const resp = await fetch(`${API_BASE}/api/admin/agents/${id}`, {
    method: 'DELETE',
    headers: { 'Authorization': 'Basic ' + btoa(`${user}:${pass}`) }
  });
  if (!resp.ok) throw new Error('Failed to delete agent');
}

export async function adminGetRunCmdBasic(user, pass, id) {
  const resp = await fetch(`${API_BASE}/api/admin/agents/${id}/run-cmd`, {
    headers: { 'Authorization': 'Basic ' + btoa(`${user}:${pass}`) }
  });
  if (!resp.ok) throw new Error('Failed to get run command');
  return resp.json();
}

export async function adminResetTokenBasic(user, pass, id) {
  const resp = await fetch(`${API_BASE}/api/admin/agents/${id}/reset-token`, {
    method: 'POST',
    headers: { 'Authorization': 'Basic ' + btoa(`${user}:${pass}`) }
  });
  if (!resp.ok) throw new Error('Failed to reset token');
  return resp.json();
}