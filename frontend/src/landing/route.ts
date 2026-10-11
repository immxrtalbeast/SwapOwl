/** Лендинг — корень сайта без хэша или с якорем (`#connect`); приложение живёт по хэш-маршрутам `#/stock`. */
export function isLandingLocation(loc: { pathname: string; hash: string }): boolean {
  if (loc.pathname !== "/") return false;
  return loc.hash === "#/" || !loc.hash.startsWith("#/");
}
