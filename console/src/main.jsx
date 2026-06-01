import React, { useEffect, useMemo, useState } from 'react';
import { createRoot } from 'react-dom/client';
import {
  Activity,
  AlertTriangle,
  Bell,
  CheckCircle2,
  ChevronRight,
  Clock3,
  Cpu,
  ExternalLink,
  FileText,
  Globe2,
  KeyRound,
  LayoutDashboard,
  ListChecks,
  Loader2,
  Megaphone,
  MonitorCheck,
  Moon,
  PlugZap,
  RadioTower,
  RefreshCcw,
  Search,
  ServerCog,
  Settings,
  ShieldCheck,
  Signal,
  TimerReset,
  UserRoundCheck,
  Workflow,
  XCircle,
} from 'lucide-react';
import './styles.css';

const nav = [
  { id: 'overview', label: 'Overview', icon: LayoutDashboard },
  { id: 'monitors', label: 'Monitors', icon: MonitorCheck },
  { id: 'incidents', label: 'Incidents', icon: AlertTriangle },
  { id: 'status', label: 'Status Pages', icon: Globe2 },
  { id: 'agents', label: 'Agents', icon: RadioTower },
  { id: 'oncall', label: 'On-call', icon: UserRoundCheck },
  { id: 'events', label: 'Event Stream', icon: Activity },
  { id: 'settings', label: 'Settings', icon: Settings },
];

const defaultMonitor = {
  name: '',
  type: 'http',
  target: '',
  method: 'GET',
  expectedStatus: 200,
  timeoutSeconds: 10,
  intervalSeconds: 60,
  failureThreshold: 3,
  enabled: true,
  regions: ['default'],
  regionConfirmationThreshold: 1,
};

function App() {
  const [view, setView] = useState('overview');
  const [token, setToken] = useState(() => localStorage.getItem('gouptime.console.token') || '');
  const [orgId, setOrgId] = useState(() => localStorage.getItem('gouptime.console.org') || '');
  const [query, setQuery] = useState('');
  const api = useMemo(() => createApi(token, orgId), [token, orgId]);
  const [state, setState] = useState({
    loading: false,
    error: '',
    health: null,
    me: null,
    stats: null,
    monitors: [],
    incidents: [],
    results: [],
    workers: null,
    statusPages: [],
    agents: [],
    schedules: [],
    runbooks: [],
  });

  async function refresh() {
    setState((s) => ({ ...s, loading: true, error: '' }));
    try {
      const health = await api.publicGet('/health');
      if (!token) {
        setState((s) => ({ ...s, health, loading: false }));
        return;
      }
      const [me, stats, monitors, incidents, results, workers, statusPages, agents, schedules, runbooks] = await Promise.all([
        api.get('/api/v1/me'),
        api.get('/api/v1/stats/overview'),
        api.get('/api/v1/monitors?limit=200'),
        api.get('/api/v1/incidents'),
        api.get('/api/v1/check-results?limit=80'),
        api.get('/api/v1/workers/status'),
        api.get('/api/v1/status-pages'),
        api.get('/api/v1/agents'),
        api.get('/api/v1/on-call/schedules'),
        api.get('/api/v1/runbooks'),
      ]);
      setState({ loading: false, error: '', health, me, stats, monitors, incidents, results, workers, statusPages, agents, schedules, runbooks });
    } catch (err) {
      setState((s) => ({ ...s, loading: false, error: err.message || 'Request failed' }));
    }
  }

  useEffect(() => {
    refresh();
  }, [token, orgId]);

  function saveAuth(nextToken, nextOrg) {
    setToken(nextToken.trim());
    setOrgId(nextOrg.trim());
    localStorage.setItem('gouptime.console.token', nextToken.trim());
    localStorage.setItem('gouptime.console.org', nextOrg.trim());
  }

  const filteredMonitors = state.monitors.filter((monitor) => {
    if (!query) return true;
    const q = query.toLowerCase();
    return [monitor.name, monitor.target, monitor.type, monitor.status].some((value) => String(value || '').toLowerCase().includes(q));
  });

  const current = nav.find((item) => item.id === view) || nav[0];

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brandMark"><Signal size={18} /></div>
          <div>
            <strong>GoUpTime</strong>
            <span>Operations console</span>
          </div>
        </div>
        <nav className="navList">
          {nav.map((item) => {
            const Icon = item.icon;
            return (
              <button key={item.id} type="button" className={view === item.id ? 'active' : ''} onClick={() => setView(item.id)}>
                <Icon size={17} />
                <span>{item.label}</span>
              </button>
            );
          })}
        </nav>
        <SystemRail health={state.health} workers={state.workers} />
      </aside>

      <main className="main">
        <header className="topbar">
          <div>
            <p className="eyebrow">{current.label}</p>
            <h1>{pageTitle(view)}</h1>
          </div>
          <div className="topActions">
            <label className="searchBox">
              <Search size={16} />
              <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search monitors" />
            </label>
            <button className="iconButton" type="button" onClick={refresh} title="Refresh">
              {state.loading ? <Loader2 size={17} className="spin" /> : <RefreshCcw size={17} />}
            </button>
          </div>
        </header>

        {state.error && <Notice tone="danger" icon={XCircle}>{state.error}</Notice>}
        {!token && <AuthPanel onSave={saveAuth} health={state.health} />}

        {token && view === 'overview' && <Overview data={state} api={api} onRefresh={refresh} />}
        {token && view === 'monitors' && <Monitors monitors={filteredMonitors} api={api} onRefresh={refresh} />}
        {token && view === 'incidents' && <Incidents incidents={state.incidents} api={api} onRefresh={refresh} />}
        {token && view === 'status' && <StatusPages pages={state.statusPages} api={api} onRefresh={refresh} />}
        {token && view === 'agents' && <Agents agents={state.agents} api={api} onRefresh={refresh} />}
        {token && view === 'oncall' && <OnCall schedules={state.schedules} api={api} onRefresh={refresh} />}
        {token && view === 'events' && <EventStream results={state.results} incidents={state.incidents} workers={state.workers} />}
        {token && view === 'settings' && <SettingsView token={token} orgId={orgId} onSave={saveAuth} data={state} />}
      </main>
    </div>
  );
}

function createApi(token, orgId) {
  async function request(path, options = {}) {
    const headers = { 'Content-Type': 'application/json', ...(options.headers || {}) };
    if (token) headers.Authorization = `Bearer ${token}`;
    if (orgId) headers['X-Org-Id'] = orgId;
    const res = await fetch(path, { ...options, headers });
    if (!res.ok) {
      let message = `${res.status} ${res.statusText}`;
      try {
        const body = await res.json();
        message = body.error || message;
      } catch {
        // leave default message
      }
      throw new Error(message);
    }
    if (res.status === 204) return null;
    const type = res.headers.get('content-type') || '';
    return type.includes('application/json') ? res.json() : res.text();
  }
  return {
    publicGet: (path) => request(path, { headers: {} }),
    get: (path) => request(path),
    post: (path, body) => request(path, { method: 'POST', body: JSON.stringify(body || {}) }),
    put: (path, body) => request(path, { method: 'PUT', body: JSON.stringify(body || {}) }),
    del: (path) => request(path, { method: 'DELETE' }),
  };
}

function pageTitle(view) {
  const titles = {
    overview: 'Control room',
    monitors: 'Checks and monitors',
    incidents: 'Incident response',
    status: 'Customer communication',
    agents: 'Private agents',
    oncall: 'Response routing',
    events: 'First-party event stream',
    settings: 'Console settings',
  };
  return titles[view] || 'Console';
}

function AuthPanel({ onSave, health }) {
  const [token, setToken] = useState('');
  const [orgId, setOrgId] = useState('');
  return (
    <section className="authPanel">
      <div>
        <p className="eyebrow">Connect</p>
        <h2>Enter an API key</h2>
        <p className="muted">The console talks directly to this GoUpTime API. Keys stay in this browser.</p>
      </div>
      <div className="authFields">
        <label>
          API key
          <input type="password" value={token} onChange={(e) => setToken(e.target.value)} placeholder="dev_admin_key_change_me" />
        </label>
        <label>
          Organization ID
          <input value={orgId} onChange={(e) => setOrgId(e.target.value)} placeholder="optional" />
        </label>
        <button className="primaryButton" type="button" onClick={() => onSave(token, orgId)}>
          <KeyRound size={16} />
          Save key
        </button>
      </div>
      <div className="healthChip">
        <span className={`dot ${health?.status === 'ok' ? 'up' : 'warn'}`} />
        API {health?.status || 'unknown'}
      </div>
    </section>
  );
}

function Overview({ data, api, onRefresh }) {
  const openIncidents = data.incidents.filter((incident) => incident.status !== 'resolved');
  const down = data.monitors.filter((monitor) => monitor.status === 'down');
  const degraded = data.monitors.filter((monitor) => monitor.status === 'degraded');
  return (
    <div className="viewStack">
      <section className="summaryGrid">
        <Metric label="Operational monitors" value={data.stats?.monitorsUp ?? data.monitors.filter((m) => m.status === 'up').length} icon={CheckCircle2} tone="good" />
        <Metric label="Open incidents" value={openIncidents.length} icon={AlertTriangle} tone={openIncidents.length ? 'bad' : 'good'} />
        <Metric label="Degraded checks" value={degraded.length} icon={TimerReset} tone={degraded.length ? 'warn' : 'good'} />
        <Metric label="Private agents" value={data.agents.length} icon={RadioTower} tone="neutral" />
      </section>

      <section className="workGrid">
        <Panel title="Incident queue" action={<SmallLink onClick={() => {}}>Live</SmallLink>}>
          <IncidentQueue incidents={openIncidents} api={api} onRefresh={onRefresh} />
        </Panel>
        <Panel title="Monitor health">
          <HealthBands monitors={data.monitors} />
          <CompactList items={[...down, ...degraded].slice(0, 6)} empty="No monitors need attention" render={(item) => (
            <div className="rowItem">
              <StatusPill status={item.status} />
              <div>
                <strong>{item.name}</strong>
                <span>{item.target || item.type}</span>
              </div>
            </div>
          )} />
        </Panel>
      </section>

      <section className="wideGrid">
        <Panel title="Recent check events">
          <EventRows results={data.results.slice(0, 12)} />
        </Panel>
        <Panel title="Response surface">
          <SurfaceMap data={data} />
        </Panel>
      </section>
    </div>
  );
}

function Monitors({ monitors, api, onRefresh }) {
  const [draft, setDraft] = useState(defaultMonitor);
  const [busy, setBusy] = useState(false);
  async function createMonitor() {
    setBusy(true);
    try {
      await api.post('/api/v1/monitors', draft);
      setDraft(defaultMonitor);
      await onRefresh();
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="viewStack">
      <Panel title="Create monitor">
        <div className="formGrid">
          <TextField label="Name" value={draft.name} onChange={(name) => setDraft({ ...draft, name })} />
          <SelectField label="Type" value={draft.type} onChange={(type) => setDraft({ ...draft, type })} options={['http', 'api', 'tcp', 'udp', 'dns', 'tls', 'keyword', 'heartbeat', 'ping', 'browser', 'domain', 'multistep']} />
          <TextField label="Target" value={draft.target} onChange={(target) => setDraft({ ...draft, target })} />
          <NumberField label="Interval" value={draft.intervalSeconds} onChange={(intervalSeconds) => setDraft({ ...draft, intervalSeconds })} />
          <NumberField label="Failures" value={draft.failureThreshold} onChange={(failureThreshold) => setDraft({ ...draft, failureThreshold })} />
          <NumberField label="Regional quorum" value={draft.regionConfirmationThreshold} onChange={(regionConfirmationThreshold) => setDraft({ ...draft, regionConfirmationThreshold })} />
        </div>
        <div className="panelActions">
          <button className="primaryButton" type="button" disabled={busy || !draft.name || (draft.type !== 'heartbeat' && !draft.target)} onClick={createMonitor}>
            {busy ? <Loader2 size={16} className="spin" /> : <MonitorCheck size={16} />}
            Create monitor
          </button>
        </div>
      </Panel>
      <DataTable
        columns={['Status', 'Name', 'Type', 'Target', 'Interval', 'Actions']}
        rows={monitors}
        render={(monitor) => (
          <tr key={monitor.id}>
            <td><StatusPill status={monitor.status} /></td>
            <td><strong>{monitor.name}</strong><span className="subline">{monitor.tags?.join(', ')}</span></td>
            <td>{monitor.type}</td>
            <td className="truncate">{monitor.target || monitor.serviceId || 'configured'}</td>
            <td>{monitor.intervalSeconds || 60}s</td>
            <td>
              <button className="ghostButton" type="button" onClick={async () => { await api.post(`/api/v1/monitors/${monitor.id}/check-now`); await onRefresh(); }}>
                <RefreshCcw size={15} /> Check
              </button>
            </td>
          </tr>
        )}
      />
    </div>
  );
}

function Incidents({ incidents, api, onRefresh }) {
  const [selected, setSelected] = useState(null);
  const [timeline, setTimeline] = useState([]);
  async function openIncident(incident) {
    setSelected(incident);
    try {
      setTimeline(await api.get(`/api/v1/incidents/${incident.id}/timeline?limit=80`));
    } catch {
      setTimeline([]);
    }
  }
  return (
    <div className="splitView">
      <Panel title="Incidents">
        <CompactList items={incidents} empty="No incidents recorded" render={(incident) => (
          <button className={`incidentButton ${selected?.id === incident.id ? 'selected' : ''}`} type="button" onClick={() => openIncident(incident)}>
            <StatusPill status={incident.status === 'resolved' ? 'up' : 'down'} label={incident.status} />
            <div>
              <strong>{incident.reason || incident.monitorId}</strong>
              <span>{incident.severity || 'major'} / {formatTime(incident.startedAt)}</span>
            </div>
            <ChevronRight size={16} />
          </button>
        )} />
      </Panel>
      <Panel title={selected ? 'Incident detail' : 'Select an incident'}>
        {selected ? (
          <div className="detailStack">
            <div className="incidentHeader">
              <div>
                <strong>{selected.reason}</strong>
                <span>{selected.impact || 'degraded'} impact</span>
              </div>
              <div className="buttonRow">
                <button className="ghostButton" type="button" onClick={async () => { await api.post(`/api/v1/incidents/${selected.id}/ack`); await onRefresh(); }}>
                  <Bell size={15} /> Ack
                </button>
                <button className="primaryButton" type="button" onClick={async () => { await api.post(`/api/v1/incidents/${selected.id}/resolve`); await onRefresh(); }}>
                  <CheckCircle2 size={15} /> Resolve
                </button>
              </div>
            </div>
            <Timeline events={timeline} />
          </div>
        ) : <EmptyState icon={AlertTriangle} text="Open an incident to see evidence and response history" />}
      </Panel>
    </div>
  );
}

function StatusPages({ pages, api, onRefresh }) {
  const [draft, setDraft] = useState({ statusPageId: '', type: 'general', title: '', body: '', status: 'published' });
  async function publish() {
    await api.post(`/api/v1/status-pages/${draft.statusPageId}/announcements`, draft);
    setDraft({ statusPageId: draft.statusPageId, type: 'general', title: '', body: '', status: 'published' });
    await onRefresh();
  }
  return (
    <div className="viewStack">
      <DataTable
        columns={['Page', 'Slug', 'Visibility', 'Auto updates', 'Open']}
        rows={pages}
        render={(page) => (
          <tr key={page.id}>
            <td><strong>{page.name}</strong><span className="subline">{page.description}</span></td>
            <td>{page.slug}</td>
            <td>{page.public || page.published ? 'public' : 'private'}</td>
            <td>{page.autoUpdates ? 'on' : 'off'}</td>
            <td><a className="tableLink" href={`/s/${page.slug}`} target="_blank" rel="noreferrer"><ExternalLink size={15} /> View</a></td>
          </tr>
        )}
      />
      <Panel title="Publish announcement">
        <div className="formGrid">
          <SelectField label="Status page" value={draft.statusPageId} onChange={(statusPageId) => setDraft({ ...draft, statusPageId })} options={pages.map((page) => ({ value: page.id, label: page.name }))} />
          <SelectField label="Type" value={draft.type} onChange={(type) => setDraft({ ...draft, type })} options={['general', 'maintenance', 'incident']} />
          <TextField label="Title" value={draft.title} onChange={(title) => setDraft({ ...draft, title })} />
          <TextField label="Body" value={draft.body} onChange={(body) => setDraft({ ...draft, body })} />
        </div>
        <div className="panelActions">
          <button className="primaryButton" type="button" disabled={!draft.statusPageId || !draft.title} onClick={publish}>
            <Megaphone size={16} /> Publish
          </button>
        </div>
      </Panel>
    </div>
  );
}

function Agents({ agents, api, onRefresh }) {
  const [draft, setDraft] = useState({ name: '', region: 'default' });
  const [token, setToken] = useState('');
  async function createAgent() {
    const result = await api.post('/api/v1/agents', draft);
    setToken(result.token);
    setDraft({ name: '', region: 'default' });
    await onRefresh();
  }
  return (
    <div className="viewStack">
      <Panel title="Provision agent">
        <div className="formGrid compact">
          <TextField label="Name" value={draft.name} onChange={(name) => setDraft({ ...draft, name })} />
          <TextField label="Region" value={draft.region} onChange={(region) => setDraft({ ...draft, region })} />
        </div>
        <div className="panelActions">
          <button className="primaryButton" type="button" disabled={!draft.name || !draft.region} onClick={createAgent}>
            <RadioTower size={16} /> Create agent
          </button>
        </div>
        {token && <Notice tone="info" icon={KeyRound}>New token: <code>{token}</code></Notice>}
      </Panel>
      <DataTable
        columns={['Agent', 'Region', 'Last seen', 'State']}
        rows={agents}
        render={(agent) => (
          <tr key={agent.id}>
            <td><strong>{agent.name}</strong><span className="subline">{agent.id}</span></td>
            <td>{agent.region}</td>
            <td>{agent.lastSeenAt ? formatTime(agent.lastSeenAt) : 'never'}</td>
            <td><StatusPill status={agent.revokedAt ? 'down' : 'up'} label={agent.revokedAt ? 'revoked' : 'active'} /></td>
          </tr>
        )}
      />
    </div>
  );
}

function OnCall({ schedules, api, onRefresh }) {
  const [draft, setDraft] = useState({ name: '', timezone: 'Australia/Sydney', participants: '', rotationSeconds: 86400, handoffAt: new Date().toISOString() });
  async function createSchedule() {
    await api.post('/api/v1/on-call/schedules', { ...draft, participants: draft.participants.split(',').map((item) => item.trim()).filter(Boolean) });
    setDraft({ ...draft, name: '', participants: '' });
    await onRefresh();
  }
  return (
    <div className="viewStack">
      <Panel title="Create schedule">
        <div className="formGrid">
          <TextField label="Name" value={draft.name} onChange={(name) => setDraft({ ...draft, name })} />
          <TextField label="Timezone" value={draft.timezone} onChange={(timezone) => setDraft({ ...draft, timezone })} />
          <TextField label="Participants" value={draft.participants} onChange={(participants) => setDraft({ ...draft, participants })} />
          <NumberField label="Rotation seconds" value={draft.rotationSeconds} onChange={(rotationSeconds) => setDraft({ ...draft, rotationSeconds })} />
        </div>
        <div className="panelActions">
          <button className="primaryButton" type="button" disabled={!draft.name || !draft.participants} onClick={createSchedule}>
            <Clock3 size={16} /> Create schedule
          </button>
        </div>
      </Panel>
      <DataTable
        columns={['Schedule', 'Timezone', 'Rotation', 'Participants']}
        rows={schedules}
        render={(schedule) => (
          <tr key={schedule.id}>
            <td><strong>{schedule.name}</strong><span className="subline">{schedule.id}</span></td>
            <td>{schedule.timezone}</td>
            <td>{Math.round((schedule.rotationSeconds || 0) / 3600)}h</td>
            <td>{schedule.participants?.join(', ')}</td>
          </tr>
        )}
      />
    </div>
  );
}

function EventStream({ results, incidents, workers }) {
  const incidentEvents = incidents.map((incident) => ({
    id: `incident-${incident.id}`,
    at: incident.updatedAt || incident.startedAt,
    title: incident.reason || incident.status,
    meta: `${incident.status} / ${incident.severity || 'major'}`,
    status: incident.status === 'resolved' ? 'up' : 'down',
  }));
  const checkEvents = results.map((result) => ({
    id: `check-${result.id}`,
    at: result.checkedAt,
    title: result.error || result.status,
    meta: `${result.monitorId} / ${result.region || 'default'} / ${result.responseTimeMs || result.totalMs || 0}ms`,
    status: result.status,
  }));
  const workerRows = (workers?.workers || []).map((worker) => ({
    id: `worker-${worker.instanceId}`,
    at: worker.lastSeenAt,
    title: worker.hostname || worker.instanceId,
    meta: `${worker.region || 'default'} / ${worker.activeJobs || 0} active`,
    status: worker.stale ? 'degraded' : 'up',
  }));
  const events = [...incidentEvents, ...checkEvents, ...workerRows].sort((a, b) => new Date(b.at || 0) - new Date(a.at || 0)).slice(0, 80);
  return (
    <Panel title="Unified event stream">
      <div className="eventList">
        {events.map((event) => (
          <div key={event.id} className="eventItem">
            <StatusDot status={event.status} />
            <div>
              <strong>{event.title}</strong>
              <span>{event.meta}</span>
            </div>
            <time>{formatTime(event.at)}</time>
          </div>
        ))}
      </div>
    </Panel>
  );
}

function SettingsView({ token, orgId, onSave, data }) {
  const [draftToken, setDraftToken] = useState(token);
  const [draftOrg, setDraftOrg] = useState(orgId);
  return (
    <div className="settingsGrid">
      <Panel title="Access">
        <div className="formGrid compact">
          <TextField label="API key" type="password" value={draftToken} onChange={setDraftToken} />
          <TextField label="Organization ID" value={draftOrg} onChange={setDraftOrg} />
        </div>
        <div className="panelActions">
          <button className="primaryButton" type="button" onClick={() => onSave(draftToken, draftOrg)}>
            <ShieldCheck size={16} /> Save
          </button>
        </div>
      </Panel>
      <Panel title="Console inventory">
        <SurfaceMap data={data} />
      </Panel>
    </div>
  );
}

function SystemRail({ health, workers }) {
  const workerCount = workers?.workers?.length || 0;
  return (
    <div className="systemRail">
      <div><Cpu size={15} /><span>API</span><StatusDot status={health?.status === 'ok' ? 'up' : 'degraded'} /></div>
      <div><ServerCog size={15} /><span>Workers</span><strong>{workerCount}</strong></div>
      <div><Moon size={15} /><span>Local console</span><StatusDot status="up" /></div>
    </div>
  );
}

function Metric({ label, value, icon: Icon, tone }) {
  return (
    <div className={`metric ${tone}`}>
      <Icon size={18} />
      <div>
        <strong>{value ?? 0}</strong>
        <span>{label}</span>
      </div>
    </div>
  );
}

function Panel({ title, children, action }) {
  return (
    <section className="panel">
      <div className="panelHeader">
        <h2>{title}</h2>
        {action}
      </div>
      {children}
    </section>
  );
}

function DataTable({ columns, rows, render }) {
  return (
    <section className="tableShell">
      <table>
        <thead>
          <tr>{columns.map((column) => <th key={column}>{column}</th>)}</tr>
        </thead>
        <tbody>
          {rows.length ? rows.map(render) : (
            <tr><td colSpan={columns.length}><EmptyState icon={ListChecks} text="No rows match this view" /></td></tr>
          )}
        </tbody>
      </table>
    </section>
  );
}

function IncidentQueue({ incidents, api, onRefresh }) {
  return (
    <CompactList items={incidents.slice(0, 5)} empty="No active incidents" render={(incident) => (
      <div className="queueItem">
        <StatusPill status="down" label={incident.severity || 'major'} />
        <div>
          <strong>{incident.reason || incident.monitorId}</strong>
          <span>{formatTime(incident.startedAt)}</span>
        </div>
        <button className="ghostButton" type="button" onClick={async () => { await api.post(`/api/v1/incidents/${incident.id}/ack`); await onRefresh(); }}>
          <Bell size={14} /> Ack
        </button>
      </div>
    )} />
  );
}

function HealthBands({ monitors }) {
  const total = Math.max(monitors.length, 1);
  const counts = {
    up: monitors.filter((item) => item.status === 'up').length,
    degraded: monitors.filter((item) => item.status === 'degraded').length,
    down: monitors.filter((item) => item.status === 'down').length,
  };
  return (
    <div className="healthBands">
      {Object.entries(counts).map(([status, count]) => (
        <div key={status} className={status} style={{ width: `${Math.max(4, (count / total) * 100)}%` }} title={`${status}: ${count}`} />
      ))}
    </div>
  );
}

function EventRows({ results }) {
  return (
    <div className="eventRows">
      {results.map((result) => (
        <div key={result.id} className="eventRow">
          <StatusDot status={result.status} />
          <div>
            <strong>{result.error || result.status}</strong>
            <span>{result.monitorId} / {result.region || 'default'}</span>
          </div>
          <time>{formatTime(result.checkedAt)}</time>
        </div>
      ))}
      {!results.length && <EmptyState icon={Activity} text="No check events yet" />}
    </div>
  );
}

function SurfaceMap({ data }) {
  const items = [
    { icon: MonitorCheck, label: 'Monitors', value: data.monitors?.length || 0 },
    { icon: AlertTriangle, label: 'Incidents', value: data.incidents?.length || 0 },
    { icon: Globe2, label: 'Status pages', value: data.statusPages?.length || 0 },
    { icon: RadioTower, label: 'Agents', value: data.agents?.length || 0 },
    { icon: UserRoundCheck, label: 'Schedules', value: data.schedules?.length || 0 },
    { icon: FileText, label: 'Runbooks', value: data.runbooks?.length || 0 },
  ];
  return (
    <div className="surfaceMap">
      {items.map((item) => {
        const Icon = item.icon;
        return (
          <div key={item.label}>
            <Icon size={16} />
            <span>{item.label}</span>
            <strong>{item.value}</strong>
          </div>
        );
      })}
    </div>
  );
}

function Timeline({ events }) {
  if (!events.length) return <EmptyState icon={Workflow} text="No timeline events recorded" />;
  return (
    <div className="timeline">
      {events.map((event) => (
        <div key={event.id} className="timelineEvent">
          <StatusDot status={event.eventType?.includes('resolved') ? 'up' : 'degraded'} />
          <div>
            <strong>{event.eventType}</strong>
            <span>{event.message || JSON.stringify(event.metadata || event.evidence || {})}</span>
          </div>
          <time>{formatTime(event.createdAt)}</time>
        </div>
      ))}
    </div>
  );
}

function CompactList({ items, empty, render }) {
  if (!items.length) return <EmptyState icon={CheckCircle2} text={empty} />;
  return <div className="compactList">{items.map(render)}</div>;
}

function EmptyState({ icon: Icon, text }) {
  return (
    <div className="emptyState">
      <Icon size={18} />
      <span>{text}</span>
    </div>
  );
}

function Notice({ children, tone = 'info', icon: Icon = PlugZap }) {
  return (
    <div className={`notice ${tone}`}>
      <Icon size={17} />
      <span>{children}</span>
    </div>
  );
}

function SmallLink({ children }) {
  return <span className="smallLink">{children}</span>;
}

function TextField({ label, value, onChange, type = 'text' }) {
  return (
    <label className="field">
      <span>{label}</span>
      <input type={type} value={value || ''} onChange={(e) => onChange(e.target.value)} />
    </label>
  );
}

function NumberField({ label, value, onChange }) {
  return (
    <label className="field">
      <span>{label}</span>
      <input type="number" value={value || 0} onChange={(e) => onChange(Number(e.target.value))} />
    </label>
  );
}

function SelectField({ label, value, onChange, options }) {
  const normalized = options.map((option) => typeof option === 'string' ? { value: option, label: option } : option);
  return (
    <label className="field">
      <span>{label}</span>
      <select value={value || ''} onChange={(e) => onChange(e.target.value)}>
        <option value="">Select</option>
        {normalized.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
      </select>
    </label>
  );
}

function StatusPill({ status, label }) {
  const normalized = status || 'unknown';
  return <span className={`statusPill ${normalized}`}>{label || normalized}</span>;
}

function StatusDot({ status }) {
  const normalized = status || 'unknown';
  return <span className={`statusDot ${normalized}`} />;
}

function formatTime(value) {
  if (!value) return 'never';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'unknown';
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(date);
}

createRoot(document.getElementById('root')).render(<App />);
