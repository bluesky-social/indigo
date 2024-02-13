const isDev =
  window.location.hostname === "localhost" ||
  window.location.hostname === "jaz1";

const BGS_HOST = `${window.location.protocol}//${window.location.hostname}:${
  isDev ? "2470" : window.location.port
}`;

export { BGS_HOST };
