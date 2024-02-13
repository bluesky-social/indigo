const isDev =
  window.location.hostname === "localhost" ||
  window.location.hostname === "jaz1";

const RELAY_HOST = `${window.location.protocol}//${window.location.hostname}:${
  isDev ? "2470" : window.location.port
}`;

export { RELAY_HOST };
