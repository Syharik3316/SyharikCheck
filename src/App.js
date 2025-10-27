import React, { useEffect, useMemo, useRef, useState } from 'react';
import { createCheck, getCheck, openResultsWS, adminListAgentsBasic, adminCreateAgentBasic, adminDeleteAgentBasic, adminGetRunCmdBasic, adminResetTokenBasic } from './services/api';
import './app.css';

const ALL_METHODS = ['http','dns','tcp','icmp','udp','whois'];
const REGIONS = [
  'FR','AT','RU','US','DE','NL','SG','GB','ES','IT','PL','UA','KZ','TR','IN','JP','KR',
  'BR','CA','AU','SE','NO','FI','DK','CZ','SK','HU','RO','BG','GR','CH','BE','PT','IE',
  'LT','LV','EE','IS','IL','AE','SA','ZA','MX','AR','CL','CO','NZ','HK','CN'
];

export default function App() {
  const [target, setTarget] = useState('');
  const [methods, setMethods] = useState(['http','dns','tcp']);
  const [taskId, setTaskId] = useState('');
  const [task, setTask] = useState(null);
  const [loading, setLoading] = useState(false);
  const wsRef = useRef(null);
  const [showAdmin, setShowAdmin] = useState(false);
  const [adminUser, setAdminUser] = useState('');
  const [adminPass, setAdminPass] = useState('');
  const [authOpen, setAuthOpen] = useState(false);
  const [isAuthed, setIsAuthed] = useState(false);
  const [agents, setAgents] = useState([]);
  const [newAgentName, setNewAgentName] = useState('');
  const [newAgentRegion, setNewAgentRegion] = useState('FR');
  const [templatesOpen, setTemplatesOpen] = useState(false);
  const [selectedTemplate, setSelectedTemplate] = useState('');
  const dropdownRef = React.useRef(null);
  const applyTemplate = (list, name) => {
    const normalized = list.map(m => (m||'').toLowerCase()).filter(m => ALL_METHODS.includes(m));
    setMethods(normalized);
    setSelectedTemplate(name || '');
    setTemplatesOpen(false);
  };
  useEffect(()=>{
    if (!templatesOpen) return;
    const onDocClick = (e)=>{ if (dropdownRef.current && !dropdownRef.current.contains(e.target)) setTemplatesOpen(false); };
    const onKey = (e)=>{ if (e.key === 'Escape') setTemplatesOpen(false); };
    document.addEventListener('mousedown', onDocClick);
    document.addEventListener('keydown', onKey);
    return ()=>{ document.removeEventListener('mousedown', onDocClick); document.removeEventListener('keydown', onKey); };
  },[templatesOpen]);

  useEffect(() => {
    wsRef.current = openResultsWS((evt) => {
      if (evt?.type === 'result' && evt.task_id && evt.task_id === taskId) {
        getCheck(taskId).then(setTask).catch(()=>{});
      }
      if (evt?.type === 'log' && evt.task_id && evt.task_id === taskId) {
        setConsoleLogs(prev => [{ time: new Date().toLocaleTimeString(), stage: evt.stage, agent: evt.agent_id, region: evt.region, message: evt.message }, ...prev].slice(0, 200));
      }
    });
    return () => { try { wsRef.current?.close(); } catch (_) {} };
  }, [taskId]);

  const onToggleMethod = (m) => {
    setMethods((prev) => prev.includes(m) ? prev.filter(x => x!==m) : [...prev, m]);
  };

  const onCreate = async () => {
    setLoading(true);
    try {
      const { task_id } = await createCheck(target, methods);
      setTaskId(task_id);
      const t = await getCheck(task_id);
      setTask(t);
    } catch (e) {
      alert('Ошибка создания задачи');
    } finally {
      setLoading(false);
    }
  };

  const [route, setRoute] = useState(() => (typeof window!=='undefined' ? window.location.pathname : '/'));
  useEffect(()=>{
    const onPop = ()=> setRoute(window.location.pathname);
    window.addEventListener('popstate', onPop);
    return ()=> window.removeEventListener('popstate', onPop);
  },[]);

  const navigate = (path)=>{ window.history.pushState({}, '', path); setRoute(path); };

  const [consoleLogs, setConsoleLogs] = useState([]);

  return (
    <div className="app">
      <div className="bg-floaters" aria-hidden="true">
        <span className="floater f1" />
        <span className="floater f2" />
        <span className="floater f3" />
        <span className="floater f4" />
        <span className="floater f5" />
        <span className="floater f6" />
        <span className="floater f7" />
        <span className="floater f8" />
      </div>
      <div className="container fade-in">
        <div className="topbar">
          <div className="brand">
            <div className="brand-title title-glow">SyharikCheck</div>
          </div>
          <div className="nav-actions">
            <button className="btn-ghost" onClick={()=> navigate('/')}>На главную</button>
            <button className="btn-ghost" onClick={()=> navigate('/metrics')}>Метрики</button>
            <button className="btn-ghost" onClick={()=> navigate('/admin')}>Админ</button>
          </div>
        </div>
        {route === '/admin' ? (
          <AdminPanel show onClose={()=> navigate('/')} />
        ) : route === '/metrics' ? (
          <MetricsPage onBack={()=> navigate('/')} />
        ) : (
        <div className="info-display">
          <div style={{ display:'flex', gap:12, alignItems:'center' }}>
            <input className="text-input text-input--wide" value={target} onChange={e=>setTarget(e.target.value)} placeholder="Например: https://example.com или example.com:443" />
            <button className="btn" disabled={loading} onClick={onCreate}>
              {loading ? (<><Spinner /> Проверка…</>) : 'Проверить'}
            </button>
          </div>
          <div style={{ marginTop: 10, display: 'flex', gap: 8, flexWrap: 'wrap', alignItems:'center' }}>
            {ALL_METHODS.map(m => (
              <label key={m} className="checkbox">
                <input type="checkbox" checked={methods.includes(m)} onChange={()=>onToggleMethod(m)} />
                <span className="mark" />
                <span>{labelForMethod(m)}</span>
              </label>
            ))}
            <div style={{ marginLeft:'auto' }}>
              <div className="dropdown" ref={dropdownRef}>
                <button className="btn-ghost" onClick={()=> setTemplatesOpen(v=>!v)}>{selectedTemplate?`Шаблоны: ${selectedTemplate}`:'Шаблоны ▾'}</button>
                {templatesOpen && (
                  <div className="dropdown-menu">
                    <div className="dropdown-title">Выберите пресет</div>
                    <button className={"dropdown-item" + (selectedTemplate==='Проверка жизни'?' active':'')} onClick={()=>applyTemplate(['http','dns','icmp'],'Проверка жизни')}>Проверка жизни</button>
                    <button className={"dropdown-item" + (selectedTemplate==='Веб‑сайт'?' active':'')} onClick={()=>applyTemplate(['http','dns','whois'],'Веб‑сайт')}>Веб‑сайт</button>
                    <button className={"dropdown-item" + (selectedTemplate==='Сеть'?' active':'')} onClick={()=>applyTemplate(['icmp','tcp','dns'],'Сеть')}>Сеть</button>
                    <button className={"dropdown-item" + (selectedTemplate==='Полная'?' active':'')} onClick={()=>applyTemplate(ALL_METHODS,'Полная')}>Полная</button>
                    <button className={"dropdown-item" + (selectedTemplate==='Быстрая'?' active':'')} onClick={()=>applyTemplate(['http','dns'],'Быстрая')}>Быстрая</button>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
        )}

      {taskId && (
        <div className="info-display">
          <div>Идентификатор задачи: <b>{taskId}</b></div>
        </div>
      )}

      {task && (
        <div className="info-display">
          <h3>Статус: <StatusBadge value={task.status} /></h3>
          <div className="pill" style={{ marginTop: 8, display:'inline-flex' }}>Цель: {formatTargetForDisplay(task.target)}</div>
          <div style={{ marginTop: 8 }}>Методы: {task.methods?.map(labelForMethod).join(', ')}</div>
          <div style={{ marginTop: 6 }}>Прогресс: {task.received_results ?? task.received}/{task.expected_results ?? task.expected}</div>
          <h4 style={{ marginTop: 12 }}>Результаты</h4>
          {!task.results || task.results.length === 0 ? (
            <div>Ожидание результатов от агентов…</div>
          ) : (
            <ResultsMatrix results={task.results} />
          )}
        </div>
      )}

      {showAdmin && (
        <div className="info-display" style={{ marginTop: 20 }}>
          <div style={{ display:'flex', justifyContent:'space-between', alignItems:'center' }}>
            <h3>Админ-панель</h3>
            <button className="btn-ghost" onClick={()=>setShowAdmin(false)}>Закрыть</button>
          </div>
          <div style={{ marginTop: 10, display:'flex', gap:12, alignItems:'center' }}>
            <input className="text-input" style={{ maxWidth: 180 }} value={adminUser} onChange={e=>setAdminUser(e.target.value)} placeholder="Логин" />
            <input className="text-input" style={{ maxWidth: 180 }} type="password" value={adminPass} onChange={e=>setAdminPass(e.target.value)} placeholder="Пароль" />
            <button className="btn" onClick={()=>{
              const u = adminUser.trim(); const p = adminPass.trim();
              adminListAgentsBasic(u, p).then(setAgents).catch(()=>alert('Неверные логин/пароль'))
            }}>Войти</button>
          </div>
          <div style={{ marginTop: 16 }}>
            <h4>Создать агента</h4>
            <div style={{ display:'flex', gap:8, alignItems:'center', marginTop:8 }}>
              <input className="text-input" placeholder="Имя агента" value={newAgentName} onChange={e=>setNewAgentName(e.target.value)} />
              <select className="text-input" style={{ maxWidth:140 }} value={newAgentRegion} onChange={e=>setNewAgentRegion(e.target.value)}>
                {REGIONS.map(r=> <option key={r} value={r}>{r}</option>)}
              </select>
              <button className="btn" onClick={async ()=>{
                try {
                  const res = await adminCreateAgentBasic(adminUser, adminPass, newAgentName, newAgentRegion);
                  try {
                    if (navigator.clipboard?.writeText) {
                      await navigator.clipboard.writeText(res.docker_cmd);
                    } else {
                      const ta = document.createElement('textarea');
                      ta.value = res.docker_cmd;
                      ta.style.position = 'fixed';
                      ta.style.opacity = '0';
                      document.body.appendChild(ta);
                      ta.focus(); ta.select();
                      try { document.execCommand('copy'); } finally { document.body.removeChild(ta); }
                    }
                  } catch (_) {}
                  alert('Команда для запуска в Docker:\n\n'+res.docker_cmd+'\n\nТокен (хвост): '+res.token_tail+'\n\n(Команда также скопирована в буфер, если это разрешено браузером)');
                  setNewAgentName('');
                  const list = await adminListAgentsBasic(adminUser, adminPass);
                  setAgents(list);
                } catch {
                  alert('Не удалось создать агента');
                }
              }}>Создать</button>
            </div>
          </div>
          <div style={{ marginTop: 16 }}>
            <h4>Список агентов</h4>
            <table>
              <thead>
                <tr>
                  <th align='left'>Имя</th>
                  <th align='left'>Регион</th>
                  <th align='left'>IP</th>
                  <th align='left'>Последний контакт</th>
                  <th align='left'>Статус</th>
                  <th align='left'>Выполнено задач</th>
                  <th align='left'>Действия</th>
                </tr>
              </thead>
              <tbody>
                {agents.map(a => (
                  <tr key={a.id}>
                    <td>{a.name}</td>
                <td>{renderRegionWithFlag(a.region)}</td>
                    <td>{a.ip || '—'}</td>
                    <td>{a.last_heartbeat || '—'}</td>
                    <td>{a.online && !a.revoked ? '🟢 онлайн' : '🔴 офлайн'}</td>
                    <td>{a.tasks_completed || 0}</td>
                    <td style={{ display:'flex', gap:8 }}>
                      <button className="btn-ghost" onClick={async ()=>{
                        try {
                          const { docker_cmd } = await adminGetRunCmdBasic(adminUser, adminPass, a.id);
                          try {
                            if (navigator.clipboard?.writeText) {
                              await navigator.clipboard.writeText(docker_cmd);
                            } else {
                              const ta = document.createElement('textarea');
                              ta.value = docker_cmd; ta.style.position='fixed'; ta.style.opacity='0';
                              document.body.appendChild(ta); ta.focus(); ta.select();
                              try { document.execCommand('copy'); } finally { document.body.removeChild(ta); }
                            }
                          } catch {}
                          alert('Команда для запуска в Docker:\n\n'+docker_cmd+'\n\n(Команда также скопирована в буфер, если это разрешено браузером)');
                        } catch { alert('Не удалось получить команду'); }
                      }}>Команда</button>
                      <button className="btn-ghost" onClick={async ()=>{
                        try { const r = await adminResetTokenBasic(adminUser, adminPass, a.id); alert('Новый токен (хвост): '+r.token_tail); } catch { alert('Не удалось перевыпустить токен'); }
                      }}>Сбросить токен</button>
                      <button className="btn-ghost" onClick={async ()=>{
                        if (!window.confirm(`Удалить агента ${a.name}?`)) return;
                        try { await adminDeleteAgentBasic(adminUser, adminPass, a.id); setAgents(await adminListAgentsBasic(adminUser, adminPass)); } catch { alert('Не удалось удалить'); }
                      }}>Удалить</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
      </div>
    </div>
  );
}

function MetricsPage({ onBack }){
  const [agents, setAgents] = useState([]);
  useEffect(()=>{
    fetch('/api/agents').then(r=>r.json()).then(setAgents).catch(()=>{});
  },[]);
  return (
    <div className="info-display">
      <div style={{ display:'flex', justifyContent:'center', alignItems:'center', marginBottom: 20 }}>
        <h3>Агенты</h3>
      </div>
      <table>
        <thead>
          <tr>
            <th align='left'>Имя</th>
            <th align='left'>Регион</th>
            <th align='left'>IP</th>
            <th align='left'>Последний контакт</th>
            <th align='left'>Статус</th>
            <th align='left'>Пинг</th>
            <th align='left'>Выполнено</th>
          </tr>
        </thead>
        <tbody>
          {agents.map(a => {
            const pingMs = a.last_heartbeat ? Math.max(0, Math.round((Date.now() - Date.parse(a.last_heartbeat)))) : null;
            return (
              <tr key={a.name}>
                <td>{a.name}</td>
                <td>{renderRegionWithFlag(a.region)}</td>
                <td>{a.ip || '—'}</td>
                <td>{a.last_heartbeat || '—'}</td>
                <td>{a.online ? '🟢 онлайн' : '🔴 офлайн'}</td>
                <td>{pingMs!=null ? `~${Math.floor(pingMs/1000)}s` : '—'}</td>
                <td>{a.tasks_completed || '—'}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function AdminPanel({ show, onClose }){
  const [adminUser, setAdminUser] = useState('');
  const [adminPass, setAdminPass] = useState('');
  const [authOpen, setAuthOpen] = useState(false);
  const [isAuthed, setIsAuthed] = useState(!!(typeof window!=='undefined' && sessionStorage.getItem('admin_auth')));
  const [agents, setAgents] = useState([]);
  const [newAgentName, setNewAgentName] = useState('');
  const [newAgentRegion, setNewAgentRegion] = useState('FR');
  const [modalOpen, setModalOpen] = useState(false);
  const [sshHost, setSshHost] = useState('');
  const [sshUser, setSshUser] = useState('root');
  const [sshPass, setSshPass] = useState('');
  const [isProvisioningAgent, setIsProvisioningAgent] = useState(false);

  const getAuth = ()=> (typeof window!=='undefined' ? sessionStorage.getItem('admin_auth') : null);
  const authHeader = ()=> ({ 'Authorization': 'Basic ' + (getAuth() || btoa((adminUser||'')+':' + (adminPass||''))) });

  useEffect(()=>{
    if (show && routeIsAdmin()) {
      if (!isAuthed) { setAuthOpen(true); return; }
      fetch('/api/admin/agents', { headers: { ...authHeader() } })
        .then(r=>{ if(!r.ok) throw new Error('auth'); return r.json(); })
        .then(setAgents)
        .catch(()=>{ setIsAuthed(false); setAuthOpen(true); });
    }
  },[show, isAuthed]);
  const routeIsAdmin = ()=> (typeof window!=='undefined' ? window.location.pathname==='/admin' : false);

  if (!show) return null;
  if (!isAuthed) {
    return (
      <div className="modal-backdrop">
        <div className="modal-sheet">
          <div style={{ display:'flex', justifyContent:'space-between', alignItems:'center' }}>
            <h3>Авторизация администратора</h3>
          </div>
          <AdminLoginActions onClose={onClose} setAgents={setAgents} setIsAuthed={setIsAuthed} />
        </div>
      </div>
    );
  }

  return (
    <div className="info-display" style={{ marginTop: 20 }}>
      <div style={{ display:'flex', justifyContent:'center', alignItems:'center', marginBottom: 20 }}>
        <h3>Админ-панель</h3>
      </div>
      <div style={{ marginTop: 16, display:'flex', justifyContent:'space-between', alignItems:'center' }}>
        <button className="btn" onClick={()=> setModalOpen(true)}>Создать агента</button>
        <button className="btn-ghost" onClick={()=>{ setIsAuthed(false); sessionStorage.removeItem('admin_auth'); onClose(); }}>Выйти</button>
      </div>
      {modalOpen && (
        <div className="modal-backdrop" onClick={(e)=>{ if (e.target.classList.contains('modal-backdrop')) setModalOpen(false) }}>
          <div className="modal-sheet">
            <div style={{ display:'flex', justifyContent:'space-between', alignItems:'center' }}>
              <h3>Новый агент</h3>
              <button className="btn-ghost" onClick={()=> setModalOpen(false)}>✕</button>
            </div>
            <div style={{ display:'flex', flexDirection:'column', gap:12, marginTop:12 }}>
              <input className="text-input" placeholder="Имя агента" value={newAgentName} onChange={e=>setNewAgentName(e.target.value)} />
              <select className="text-input" value={newAgentRegion} onChange={e=>setNewAgentRegion(e.target.value)}>
                {REGIONS.map(r=> <option key={r} value={r}>{r}</option>)}
              </select>
              <input className="text-input" placeholder="IP адрес (SSH)" value={sshHost} onChange={e=>setSshHost(e.target.value)} />
              <input className="text-input" placeholder="Пользователь (SSH)" value={sshUser} onChange={e=>setSshUser(e.target.value)} />
              <input className="text-input" type="password" placeholder="Пароль (SSH)" value={sshPass} onChange={e=>setSshPass(e.target.value)} />
            </div>
            <div style={{ display:'flex', justifyContent:'flex-end', gap:8, marginTop:16 }}>
              <button className="btn-ghost" onClick={()=> setModalOpen(false)}>Отмена</button>
              <button className="btn" disabled={isProvisioningAgent} onClick={async ()=>{
                if (isProvisioningAgent) return;
                const payload = { name:newAgentName.trim(), region:newAgentRegion, ssh_host:sshHost.trim(), ssh_user:sshUser.trim(), ssh_pass:sshPass };
                if (!payload.name || !payload.region || !payload.ssh_host || !payload.ssh_user || !payload.ssh_pass) { alert('Заполните все поля'); return; }
                setIsProvisioningAgent(true);
                try {
                  const resp = await fetch('/api/admin/agents/provision', { method:'POST', headers:{ 'Content-Type':'application/json', ...authHeader() }, body: JSON.stringify(payload)});
                  if (!resp.ok) throw new Error('bad');
                  await resp.json();
                  alert('Агент успешно добавлен и развёрнут.');
                  setModalOpen(false);
                  setNewAgentName(''); setSshHost(''); setSshUser('root'); setSshPass('');
                  const list = await fetch('/api/admin/agents', { headers:{ ...authHeader() } }).then(r=>r.json());
                  setAgents(list);
                } catch {
                  alert('Не удалось создать и развернуть агента');
                } finally {
                  setIsProvisioningAgent(false);
                }
              }}>{isProvisioningAgent ? 'Создание…' : 'Создать'}</button>
            </div>
          </div>
        </div>
      )}
      <div style={{ marginTop: 16 }}>
        <h4>Список агентов</h4>
        <table>
          <thead>
            <tr>
              <th align='left'>Имя</th>
              <th align='left'>Регион</th>
              <th align='left'>IP</th>
              <th align='left'>Последний контакт</th>
              <th align='left'>Статус</th>
              <th align='left'>Выполнено задач</th>
              <th align='left'>Действия</th>
            </tr>
          </thead>
          <tbody>
            {agents.map(a => (
              <tr key={a.id}>
                <td>{a.name}</td>
                <td>{renderRegionWithFlag(a.region)}</td>
                <td>{a.ip || '—'}</td>
                <td>{a.last_heartbeat || '—'}</td>
                <td>{a.online && !a.revoked ? '🟢 онлайн' : '🔴 офлайн'}</td>
                <td>{a.tasks_completed || 0}</td>
                <td style={{ display:'flex', gap:8 }}>
                  <button className="btn-ghost" onClick={async ()=>{
                    try {
                      const r = await fetch(`/api/admin/agents/${a.id}/run-cmd`, { headers:{ ...authHeader() } });
                      if (!r.ok) throw new Error();
                      const { docker_cmd } = await r.json();
                      try {
                        if (navigator.clipboard?.writeText) { await navigator.clipboard.writeText(docker_cmd); }
                        else {
                          const ta = document.createElement('textarea'); ta.value = docker_cmd; ta.style.position='fixed'; ta.style.opacity='0';
                          document.body.appendChild(ta); ta.focus(); ta.select(); try { document.execCommand('copy'); } finally { document.body.removeChild(ta); }
                        }
                      } catch {}
                      alert('Команда для запуска:\n\n'+docker_cmd+'\n\n(Команда также скопирована в буфер, если это разрешено браузером)');
                    } catch { alert('Не удалось получить команду'); }
                  }}>Команда</button>
                  <button className="btn-ghost" onClick={async ()=>{
                    if (!window.confirm(`Удалить агента ${a.name}?`)) return;
                    try {
                      const r = await fetch(`/api/admin/agents/${a.id}`, { method:'DELETE', headers:{ ...authHeader() } });
                      if (!r.ok) throw new Error();
                      const list = await fetch('/api/admin/agents', { headers:{ ...authHeader() } }).then(r=>r.json());
                      setAgents(list);
                    } catch { alert('Не удалось удалить'); }
                  }}>Удалить</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function StatusBadge({ value }) {
  const v = (value||'').toLowerCase();
  const map = { queued: 'В очереди', running: 'Выполняется', finished: 'Завершено', failed: 'Ошибка' };
  const color = v==='finished' ? '#16a34a' : v==='running' ? '#2563eb' : v==='failed' ? '#dc2626' : '#6b7280';
  return <span style={{ padding:'2px 8px', borderRadius:8, background: color, color:'#fff' }}>{map[v] || '—'}</span>;
}

function MethodGroups({ results }) {
  const groups = results.reduce((acc, r) => {
    const k = r.method || 'unknown';
    (acc[k] = acc[k] || []).push(r);
    return acc;
  }, {});
  const order = ['http','dns','tcp','icmp','udp','whois'];
  return (
    <div className="cards">
      {Object.keys(groups)
        .filter((k)=> k !== 'traceroute')
        .sort((a,b)=>order.indexOf(a)-order.indexOf(b))
        .map(method => (
        <div key={method} className="card">
          <h5>{labelForMethod(method)}</h5>
          <div className="card-content-scroll">
            <table>
              <thead>
              <tr>
                <th align='left'>Агент</th>
                <th align='left'>Регион</th>
                <th align='left'>Состояние</th>
              </tr>
            </thead>
            <tbody>
              {groups[method].sort((a,b)=>String(a.region||'').localeCompare(String(b.region||''))).map((r, idx) => (
                <tr key={r.id || idx}>
                  <td>{r.agent_id || '—'}</td>
                  <td>{renderRegionWithFlag(r.region)}</td>
                  <td>{renderResult(r)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          </div>
        </div>
      ))}
    </div>
  );
}

function ResultsMatrix({ results }) {
  const order = ['http','dns','tcp','icmp','udp','whois'];
  const methodSet = Array.from(new Set((results||[]).map(r => r.method))).filter(m=>m && m!=='traceroute');
  const methods = methodSet.sort((a,b)=> order.indexOf(a) - order.indexOf(b));

  const rowsMap = new Map();
  (results||[]).forEach(r => {
    if (!r || r.method === 'traceroute') return;
    const key = String(r.agent_id || '') + '|' + String(r.region || '');
    if (!rowsMap.has(key)) {
      rowsMap.set(key, { agent: r.agent_id || '—', region: r.region || '—', byMethod: {} });
    }
    const row = rowsMap.get(key);
    row.byMethod[r.method] = r;
  });
  const rows = Array.from(rowsMap.values()).sort((a,b)=> String(a.region).localeCompare(String(b.region)) || String(a.agent).localeCompare(String(b.agent)));

  if (methods.length === 0) return <div>Нет данных для отображения.</div>;

  return (
    <div className="card" style={{ height:'auto' }}>
      <h5>Результаты по методам</h5>
      <div className="card-content-scroll" style={{ overflow:'visible' }}>
        <table>
          <thead>
            <tr>
              <th align='left'>Агент</th>
              <th align='left'>Регион</th>
              {methods.map(m => (
                <th key={m} align='left'>{labelForMethod(m)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, idx) => (
              <tr key={row.agent + '|' + row.region + '|' + idx}>
                <td>{row.agent}</td>
                <td>{renderRegionWithFlag(row.region)}</td>
                {methods.map(m => (
                  <td key={m}>
                    {row.byMethod[m] ? (
                      <div style={{ display:'block', maxWidth:'100%' }}>
                        {renderResult(row.byMethod[m])}
                      </div>
                    ) : '—'}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function labelForMethod(m) {
  switch ((m||'').toLowerCase()) {
    case 'http': return 'HTTP доступность';
    case 'dns': return 'DNS разрешение';
    case 'tcp': return 'TCP доступность порта';
    case 'icmp': return 'ICMP ping';
    case 'udp': return 'UDP доступность';
    case 'whois': return 'WHOIS информация';
    default: return m;
  }
}

function regionLabel(r) {
  const map = {
    FR:'Франция', AT:'Австрия', RU:'Россия', US:'США', DE:'Германия', NL:'Нидерланды', SG:'Сингапур',
    GB:'Великобритания', ES:'Испания', IT:'Италия', PL:'Польша', UA:'Украина', KZ:'Казахстан', TR:'Турция',
    IN:'Индия', JP:'Япония', KR:'Южная Корея', BR:'Бразилия', CA:'Канада', AU:'Австралия', SE:'Швеция',
    NO:'Норвегия', FI:'Финляндия', DK:'Дания', CZ:'Чехия', SK:'Словакия', HU:'Венгрия', RO:'Румыния',
    BG:'Болгария', GR:'Греция', CH:'Швейцария', BE:'Бельгия', PT:'Португалия', IE:'Ирландия', LT:'Литва',
    LV:'Латвия', EE:'Эстония', IS:'Исландия', IL:'Израиль', AE:'ОАЭ', SA:'Саудовская Аравия', ZA:'ЮАР',
    MX:'Мексика', AR:'Аргентина', CL:'Чили', CO:'Колумбия', NZ:'Новая Зеландия', HK:'Гонконг', CN:'Китай'
  };
  return map[r] || r || '—';
}

function renderResult(r) {
  if (!r) return '—';
  const ok = !!r.success;
  const color = ok ? '#16a34a' : '#dc2626';
  const parts = [];
  if (r.status_code) parts.push(`код ${r.status_code}`);
  if (r.latency_ms !== undefined) parts.push(`${r.latency_ms} мс`);
  if (r.message) parts.push(r.message);
  return (
    <span style={{ color }}>
      {ok ? 'OK' : 'Ошибка'}{parts.length?`: ${parts.join(', ')}`:''} {ok ? '✅' : '❌'}
      {r.details && r.method==='dns' && (
        <div style={{ color:'#e5e7eb', marginTop:6 }}>
          {Object.entries(r.details).map(([k,v])=> (
            <div key={k}><b>{k}:</b> {Array.isArray(v)? v.join(', ') : String(v)}</div>
          ))}
        </div>
      )}
      {/* HTTP headers hidden per request; show only status code above */}
      {r.details && r.method==='whois' && r.details.geoip && r.details.geoip.latitude && r.details.geoip.longitude && (
        <MiniMap lat={Number(r.details.geoip.latitude)} lon={Number(r.details.geoip.longitude)} />
      )}
      
    </span>
  );
}

function MiniMap({ lat, lon }){
  const ref = React.useRef(null);
  useEffect(()=>{
    if (!ref.current || !window.L) return;
    const map = window.L.map(ref.current, { zoomControl:false, attributionControl:true }).setView([lat, lon], 5);
    window.L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', { attribution: '&copy; OpenStreetMap' }).addTo(map);
    window.L.marker([lat, lon]).addTo(map);
    return ()=> map.remove();
  },[lat, lon]);
  return <div style={{ height:240, marginTop:6, borderRadius:8 }} ref={ref} />;
}

function formatTargetForDisplay(raw) {
  const t = String(raw || '');
  if (!t) return t;
  const hasExplicitPort = (() => {
    try {
      if (/^https?:\/\//i.test(t)) {
        const u = new URL(t);
        return !!u.port;
      }
      const lastColon = t.lastIndexOf(':');
      if (lastColon > -1) {
        const after = t.slice(lastColon + 1);
        return /^\d+$/.test(after);
      }
      return false;
    } catch (_) { return false; }
  })();
  if (hasExplicitPort) return t;

  const scheme = /^https:\/\//i.test(t) ? 'https' : /^http:\/\//i.test(t) ? 'http' : 'https';
  const defaultPort = scheme === 'https' ? '443' : '80';

  if (/^https?:\/\//i.test(t)) {
    try {
      const u = new URL(t);
      const base = `${u.protocol}//${u.hostname}:${defaultPort}`;
      return base + `${u.pathname || ''}${u.search || ''}${u.hash || ''}`;
    } catch {
      return t;
    }
  }
  return `${t}:${defaultPort}`;
}

function flagEmojiFromCode(code) {
  const c = String(code || '').trim().toUpperCase();
  if (!/^[A-Z]{2}$/.test(c)) return '🏳️';
  const A = 0x1F1E6;
  const offset = 'A'.charCodeAt(0);
  const first = A + (c.charCodeAt(0) - offset);
  const second = A + (c.charCodeAt(1) - offset);
  try { return String.fromCodePoint(first) + String.fromCodePoint(second); } catch { return '🏳️'; }
}

function renderRegionWithFlag(regionCode) {
  const flag = flagEmojiFromCode(regionCode);
  return <span>{flag} {regionLabel(regionCode)}</span>;
}

function Spinner(){
  return <span className="spinner" aria-hidden="true" />;
}

function AdminLoginActions({ onClose, setAgents, setIsAuthed }){
  const [isLoggingIn, setIsLoggingIn] = useState(false);
  const [adminUser, setAdminUser] = useState('');
  const [adminPass, setAdminPass] = useState('');
  return (
    <div>
      <div style={{ display:'flex', flexDirection:'column', gap:12, marginTop:12 }}>
        <input className="text-input" placeholder="Логин" value={adminUser} onChange={e=>setAdminUser(e.target.value)} />
        <input className="text-input" type="password" placeholder="Пароль" value={adminPass} onChange={e=>setAdminPass(e.target.value)} />
      </div>
      <div style={{ display:'flex', justifyContent:'flex-end', gap:8, marginTop:16 }}>
        <button className="btn-ghost" onClick={onClose}>Отмена</button>
        <button className="btn" disabled={isLoggingIn} onClick={async ()=>{
          if (isLoggingIn) return;
          const u = adminUser.trim(); const p = adminPass.trim();
          if (!u || !p) { alert('Введите логин и пароль'); return; }
          setIsLoggingIn(true);
          try {
            const token = btoa(u+':'+p);
            sessionStorage.setItem('admin_auth', token);
            const r = await fetch('/api/admin/agents', { headers:{ 'Authorization': 'Basic '+ token } });
            if (!r.ok) throw new Error('auth');
            setAgents(await r.json());
            setIsAuthed(true);
          } catch { alert('Неверные логин или пароль'); setIsAuthed(false); }
          finally { setIsLoggingIn(false); }
        }}>{isLoggingIn ? (<><Spinner /> Вход…</>) : 'Войти'}</button>
      </div>
    </div>
  );
}

