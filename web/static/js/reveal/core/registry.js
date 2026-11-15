// Registre des styles de révélation. Chaque phase de style (cadeau, enveloppe,
// carte à gratter) appelle registerStyle('gift' | 'envelope' | 'scratch', {
// mount(stage, ctx) }) depuis son propre module, chargé par la page.
export const styles = {};

export function registerStyle(name, style) {
  styles[name] = style;
}
