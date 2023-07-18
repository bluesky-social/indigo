import { FC, useEffect, useState } from "react";
import Alert from "../Alert/Alert";
import {
  ArrowPathIcon,
  CheckCircleIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  XCircleIcon,
  XMarkIcon,
} from "@heroicons/react/24/solid";

import { BGS_HOST, ADMIN_TOKEN } from "../../constants";
import { PDS, PDSKey } from "../../models/pds";

import ConfirmModal from "./ConfirmModal";

const Dash: FC<{}> = () => {
  const [pdsList, setPDSList] = useState<PDS[] | null>(null);
  const [alert, setAlert] = useState<Alert | null>(null);
  const [sortField, setSortField] = useState<PDSKey>("ID");
  const [sortOrder, setSortOrder] = useState<string>("asc");

  // Modal state management
  const [modalAction, setModalAction] = useState<{
    type: "block" | "disconnect";
    pds: PDS;
  } | null>(null);
  const [modalConfirm, setModalConfirm] = useState<() => void>(() => {});
  const [modalCancel, setModalCancel] = useState<() => void>(() => {});

  useEffect(() => {
    document.title = "BGS Admin Dashboard";
  }, []);

  const refreshPDSList = () => {
    fetch(`${BGS_HOST}/admin/pds/list`, {
      method: "GET",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${ADMIN_TOKEN}`,
      },
    })
      .then((res) => res.json())
      .then((res: PDS[]) => {
        setPDSList(res);
      })
      .catch((err) => {
        setAlert({
          type: "error",
          message: `Failed to fetch PDS list: ${err}`,
          dismissAlert: () => {
            setAlert(null);
          },
        });
      });
  };

  const requestCrawlHost = (host: string) => {
    fetch(
      `${BGS_HOST}/xrpc/com.atproto.sync.requestCrawl?hostname=${host}`
    ).then((res) => {
      if (res.status !== 200) {
        setAlert({
          type: "error",
          message: `Failed to request crawl: ${res.statusText} (${res.status})`,
          dismissAlert: () => {
            setAlert(null);
          },
        });
      } else {
        setAlert({
          type: "success",
          message: "Successfully requested crawl",
          dismissAlert: () => {
            setAlert(null);
          },
        });
      }
      refreshPDSList();
    });
  };

  const requestDisconnectHost = (host: string, shouldBlock: boolean) => {
    fetch(
      `${BGS_HOST}/admin/subs/killUpstream?host=${host}&block=${shouldBlock}`,
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${ADMIN_TOKEN}`,
        },
      }
    ).then((res) => {
      if (res.status !== 200) {
        setAlert({
          type: "error",
          message: `Failed to request disconnect: ${res.statusText} (${res.status})`,
          dismissAlert: () => {
            setAlert(null);
          },
        });
      } else {
        setAlert({
          type: "success",
          message: "Successfully requested disconnect",
          dismissAlert: () => {
            setAlert(null);
          },
        });
      }
      refreshPDSList();
    });
  };

  const requestUnblockHost = (host: string) => {
    fetch(`${BGS_HOST}/admin/pds/unblock?host=${host}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${ADMIN_TOKEN}`,
      },
    }).then((res) => {
      if (res.status !== 200) {
        setAlert({
          type: "error",
          message: `Failed to request unblock: ${res.statusText} (${res.status})`,
          dismissAlert: () => {
            setAlert(null);
          },
        });
      } else {
        setAlert({
          type: "success",
          message: "Successfully requested unblock",
          dismissAlert: () => {
            setAlert(null);
          },
        });
      }
      refreshPDSList();
    });
  };

  const handleBlockClick = (pds: PDS, shouldBlock: boolean) => {
    setModalAction({
      type: shouldBlock ? "block" : "disconnect",
      pds,
    });

    setModalConfirm(() => {
      return () => {
        console.log(shouldBlock ? "Blocking" : "Disconnecting");
        requestDisconnectHost(pds.Host, shouldBlock);
        setModalAction(null);
      };
    });

    setModalCancel(() => {
      return () => {
        setModalAction(null);
      };
    });
  };

  useEffect(() => {
    if (!pdsList) {
      return;
    }
    const sortedPDSs: PDS[] = [...pdsList].sort((a, b) => {
      if (sortOrder === "asc") {
        if (a[sortField]! < b[sortField]!) {
          return -1;
        }
        if (a[sortField]! > b[sortField]!) {
          return 1;
        }
      } else {
        if (a[sortField]! < b[sortField]!) {
          return 1;
        }
        if (a[sortField]! > b[sortField]!) {
          return -1;
        }
      }
      return 0;
    });
    setPDSList(sortedPDSs);
  }, [sortOrder, sortField]);

  useEffect(() => {
    refreshPDSList();
    // Refresh stats every 10 seconds
    const interval = setInterval(() => {
      refreshPDSList();
    }, 10 * 1000);

    return () => clearInterval(interval);
  }, []);

  return (
    <div className="mx-auto max-w-full">
      <div className="sm:flex sm:items-center">
        <div className="sm:flex-auto">
          <h1 className="text-2xl font-semibold leading-6 text-gray-900">
            PDS Connections
          </h1>
          <p className="mt-2 text-sm text-gray-700">
            A list of all PDS connections and their current status.
          </p>
        </div>
        <div className="">
          {alert && (
            <Alert
              type={alert.type}
              message={alert.message}
              dismissAlert={alert.dismissAlert}
              autoDismiss={alert.autoDismiss}
            />
          )}
        </div>
      </div>

      <div className="mt-8 flow-root">
        <div className="overflow-hidden shadow ring-1 ring-black ring-opacity-5 sm:rounded-lg sm:rounded-b-none">
          <table className="min-w-full divide-y divide-gray-300">
            <thead className="bg-gray-50">
              <tr>
                <th
                  scope="col"
                  className="py-3.5 pl-4 pr-3 text-left text-sm font-semibold text-gray-900 sm:pl-6"
                >
                  <a
                    href="#"
                    className="group inline-flex"
                    onClick={() => {
                      setSortField("ID");
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    ID
                    <span
                      className={`ml-2 flex-none rounded text-gray-400 ${
                        sortField === "ID"
                          ? "group-hover:bg-gray-200"
                          : "invisible group-hover:visible group-focus:visible"
                      }`}
                    >
                      {sortField === "ID" && sortOrder === "asc" ? (
                        <ChevronUpIcon className="h-5 w-5" aria-hidden="true" />
                      ) : (
                        <ChevronDownIcon
                          className="h-5 w-5"
                          aria-hidden="true"
                        />
                      )}
                    </span>
                  </a>
                </th>
                <th
                  scope="col"
                  className="px-3 py-3.5 text-left text-sm font-semibold text-gray-900"
                >
                  <a
                    href="#"
                    className="group inline-flex"
                    onClick={() => {
                      setSortField("Host");
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    Host
                    <span
                      className={`ml-2 flex-none rounded text-gray-400 ${
                        sortField === "Host"
                          ? "group-hover:bg-gray-200"
                          : "invisible group-hover:visible group-focus:visible"
                      }`}
                    >
                      {sortField === "Host" && sortOrder === "asc" ? (
                        <ChevronUpIcon className="h-5 w-5" aria-hidden="true" />
                      ) : (
                        <ChevronDownIcon
                          className="h-5 w-5"
                          aria-hidden="true"
                        />
                      )}
                    </span>
                  </a>
                </th>
                <th
                  scope="col"
                  className="px-3 py-3.5 text-right text-sm font-semibold text-gray-900 pr-6 whitespace-nowrap"
                >
                  <a
                    href="#"
                    className="group inline-flex"
                    onClick={() => {
                      setSortField("HasActiveConnection");
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    Connected
                    <span
                      className={`ml-2 flex-none rounded text-gray-400 ${
                        sortField === "HasActiveConnection"
                          ? "group-hover:bg-gray-200"
                          : "invisible group-hover:visible group-focus:visible"
                      }`}
                    >
                      {sortField === "HasActiveConnection" &&
                      sortOrder === "asc" ? (
                        <ChevronUpIcon className="h-5 w-5" aria-hidden="true" />
                      ) : (
                        <ChevronDownIcon
                          className="h-5 w-5"
                          aria-hidden="true"
                        />
                      )}
                    </span>
                  </a>
                </th>
                <th
                  scope="col"
                  className="px-3 py-3.5 text-right text-sm font-semibold text-gray-900 pr-6 whitespace-nowrap"
                >
                  <a
                    href="#"
                    className="group inline-flex"
                    onClick={() => {
                      setSortField("Blocked");
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    Permitted
                    <span
                      className={`ml-2 flex-none rounded text-gray-400 ${
                        sortField === "Blocked"
                          ? "group-hover:bg-gray-200"
                          : "invisible group-hover:visible group-focus:visible"
                      }`}
                    >
                      {sortField === "Blocked" && sortOrder === "asc" ? (
                        <ChevronUpIcon className="h-5 w-5" aria-hidden="true" />
                      ) : (
                        <ChevronDownIcon
                          className="h-5 w-5"
                          aria-hidden="true"
                        />
                      )}
                    </span>
                  </a>
                </th>
                <th
                  scope="col"
                  className="px-3 py-3.5 text-right text-sm font-semibold text-gray-900 pr-6 whitespace-nowrap"
                >
                  <a
                    href="#"
                    className="group inline-flex"
                    onClick={() => {
                      setSortField("EventsSeenSinceStartup");
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    Events Seen
                    <span
                      className={`ml-2 flex-none rounded text-gray-400 ${
                        sortField === "EventsSeenSinceStartup"
                          ? "group-hover:bg-gray-200"
                          : "invisible group-hover:visible group-focus:visible"
                      }`}
                    >
                      {sortField === "EventsSeenSinceStartup" &&
                      sortOrder === "asc" ? (
                        <ChevronUpIcon className="h-5 w-5" aria-hidden="true" />
                      ) : (
                        <ChevronDownIcon
                          className="h-5 w-5"
                          aria-hidden="true"
                        />
                      )}
                    </span>
                  </a>
                </th>
                <th
                  scope="col"
                  className="px-3 py-3.5 text-right text-sm font-semibold text-gray-900 pr-6 whitespace-nowrap"
                >
                  <a
                    href="#"
                    className="group inline-flex"
                    onClick={() => {
                      setSortField("Cursor");
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    Cursor
                    <span
                      className={`ml-2 flex-none rounded text-gray-400 ${
                        sortField === "Cursor"
                          ? "group-hover:bg-gray-200"
                          : "invisible group-hover:visible group-focus:visible"
                      }`}
                    >
                      {sortField === "Cursor" && sortOrder === "asc" ? (
                        <ChevronUpIcon className="h-5 w-5" aria-hidden="true" />
                      ) : (
                        <ChevronDownIcon
                          className="h-5 w-5"
                          aria-hidden="true"
                        />
                      )}
                    </span>
                  </a>
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200 bg-white">
              {pdsList &&
                pdsList.map((pds) => {
                  return (
                    <tr key={pds.ID}>
                      <td className="whitespace-nowrap py-4 pl-4 pr-3 text-sm font-medium text-gray-900 sm:pl-6 text-left">
                        {pds.ID}
                      </td>
                      <td className="whitespace-nowrap px-3 py-4 text-sm text-gray-500 text-left">
                        {pds.Host}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 w-8 pr-6">
                        {pds.HasActiveConnection ? (
                          <div className="inline-flex justify-center w-full">
                            <CheckCircleIcon
                              className="h-5 w-5 text-green-500 my-auto mr-2"
                              aria-hidden="true"
                            />
                            <button
                              className="rounded-md p-1.5 hover:text-red-600 hover:bg-red-100 focus:outline-none focus:ring-2 focus:ring-red-600 focus:ring-offset-2 focus:ring-offset-red-50"
                              onClick={() => {
                                handleBlockClick(pds, false);
                              }}
                            >
                              <XMarkIcon
                                className="h-5 w-5"
                                aria-hidden="true"
                              />
                            </button>
                          </div>
                        ) : (
                          <div className="inline-flex justify-center w-full">
                            <XCircleIcon
                              className="h-5 w-5 text-red-500 mr-2 my-auto"
                              aria-hidden="true"
                            />
                            <button
                              className="rounded-md p-1.5 hover:text-green-600 hover:bg-green-100 focus:outline-none focus:ring-2 focus:ring-green-600 focus:ring-offset-2 focus:ring-offset-green-50"
                              onClick={() => {
                                requestCrawlHost(pds.Host);
                              }}
                            >
                              <ArrowPathIcon
                                className="h-5 w-5"
                                aria-hidden="true"
                              />
                            </button>
                          </div>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 w-8 pr-6">
                        {pds.Blocked ? (
                          <div className="inline-flex justify-center w-full">
                            <XCircleIcon
                              className="h-5 w-5 text-red-500 my-auto mr-2"
                              aria-hidden="true"
                            />
                            <button
                              className="rounded-md p-1.5 hover:text-green-600 hover:bg-green-100 focus:outline-none focus:ring-2 focus:ring-green-600 focus:ring-offset-2 focus:ring-offset-green-50"
                              onClick={() => {
                                requestUnblockHost(pds.Host);
                              }}
                            >
                              <CheckIcon
                                className="h-5 w-5"
                                aria-hidden="true"
                              />
                            </button>
                          </div>
                        ) : (
                          <div className="inline-flex justify-center w-full">
                            <CheckCircleIcon
                              className="h-5 w-5 text-green-500 my-auto mr-2"
                              aria-hidden="true"
                            />
                            <button
                              className="rounded-md p-1.5 hover:text-red-600 hover:bg-red-100 focus:outline-none focus:ring-2 focus:ring-red-600 focus:ring-offset-2 focus:ring-offset-red-50"
                              onClick={() => {
                                handleBlockClick(pds, true);
                              }}
                            >
                              <XMarkIcon
                                className="h-5 w-5"
                                aria-hidden="true"
                              />
                            </button>
                          </div>
                        )}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 text-center w-8 pr-6">
                        {pds.EventsSeenSinceStartup?.toLocaleString()}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 text-center w-8 pr-6">
                        {pds.Cursor?.toLocaleString()}
                      </td>
                    </tr>
                  );
                })}
            </tbody>
          </table>
        </div>
      </div>
      {modalAction && (
        <ConfirmModal
          action={modalAction}
          onConfirm={modalConfirm}
          onCancel={modalCancel}
        />
      )}
    </div>
  );
};

export default Dash;
