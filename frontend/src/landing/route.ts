/** Лендинг — корень сайта без хэша; приложение живёт по хэш-маршрутам (`/#/stock`). */
export function isLandingLocation(loc: { pathname: string; hash: string }): boolean {
  return loc.pathname === "/" && (loc.hash === "" || loc.hash === "#" || loc.hash === "#/");
}
