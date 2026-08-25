import { Stethoscope, Moon, Settings, Sun, LayoutPanelLeft } from 'lucide-react';

type Destination = 'sites' | 'doctor' | 'settings';
export function ActivityRail({ active, onNavigate, dark, onTheme }: { active: Destination; onNavigate: (d: Destination) => void; dark: boolean; onTheme: () => void }) {
  const items = [{ id: 'sites' as const, label: 'Sites', Icon: LayoutPanelLeft }, { id: 'doctor' as const, label: 'Diagnóstico', Icon: Stethoscope }, { id: 'settings' as const, label: 'Configurações', Icon: Settings }];
  return <aside className="activity-rail" aria-label="Navegação principal"><div className="brand" aria-label="DevLAN">DL</div><nav>{items.map(({ id, label, Icon }) => <button key={id} title={label} aria-label={label} className={active === id ? 'rail-item active' : 'rail-item'} onClick={() => onNavigate(id)}><Icon size={19}/></button>)}</nav><div className="rail-bottom"><button className="rail-item" title="Alternar tema" aria-label="Alternar tema" onClick={onTheme}>{dark ? <Sun size={18}/> : <Moon size={18}/>}</button><small>v0.1</small></div></aside>;
}
