import { Stethoscope, Moon, Settings, Sun, LayoutPanelLeft, Menu } from 'lucide-react';

type Destination = 'sites' | 'doctor' | 'settings';
export function ActivityRail({ active, onNavigate, dark, onTheme, onMenu }: { active: Destination; onNavigate: (d: Destination) => void; dark: boolean; onTheme: () => void; onMenu: () => void }) {
  const items = [{ id: 'sites' as const, label: 'Sites', Icon: LayoutPanelLeft }, { id: 'doctor' as const, label: 'Diagnóstico', Icon: Stethoscope }, { id: 'settings' as const, label: 'Configurações', Icon: Settings }];
  return <aside className="activity-rail" aria-label="Navegação principal"><div className="brand" aria-label="DevLAN">DL</div><nav><button className="rail-item mobile-menu" title="Abrir sites" aria-label="Abrir sites" onClick={onMenu}><Menu size={19}/></button>{items.map(({ id, label, Icon }) => <button key={id} title={label} aria-label={label} className={active === id ? 'rail-item active' : 'rail-item'} onClick={() => onNavigate(id)}><Icon size={19}/></button>)}</nav><div className="rail-bottom"><button className="rail-item" title={dark ? 'Usar tema claro' : 'Usar tema escuro'} aria-label={dark ? 'Usar tema claro' : 'Usar tema escuro'} onClick={onTheme}>{dark ? <Sun size={18}/> : <Moon size={18}/>}</button><small>v0.1</small></div></aside>;
}
