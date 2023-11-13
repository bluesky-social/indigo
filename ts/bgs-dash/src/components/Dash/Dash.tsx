import {
  ShieldCheckIcon,
  ShieldExclamationIcon,
} from "@heroicons/react/24/outline";
import {
  ArrowPathIcon,
  CheckCircleIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronUpIcon,
  PencilSquareIcon,
  XCircleIcon,
  XMarkIcon,
} from "@heroicons/react/24/solid";
import { FC, useEffect, useState } from "react";
import Notification, {
  NotificationMeta,
  NotificationType,
} from "../Notification/Notification";

import { BGS_HOST } from "../../constants";
import { PDS, PDSKey } from "../../models/pds";

import { Switch } from "@headlessui/react";
import { useNavigate } from "react-router-dom";
import ConfirmModal from "./ConfirmModal";

function classNames(...classes: string[]) {
  return classes.filter(Boolean).join(" ");
}

const Dash: FC<{}> = () => {
  const [pdsList, setPDSList] = useState<PDS[] | null>(null);
  const [sortField, setSortField] = useState<PDSKey>("ID");
  const [sortOrder, setSortOrder] = useState<string>("asc");

  // Slurp Toggle Management
  const [slurpsEnabled, setSlurpsEnabled] = useState<boolean>(true);
  const [canToggleSlurps, setCanToggleSlurps] = useState<boolean>(true);

  // Notification Management
  const [shouldShowNotification, setShouldShowNotification] =
    useState<boolean>(false);
  const [notification, setNotification] = useState<NotificationMeta>({
    message: "",
    alertType: "",
  });

  // Modal state management
  const [modalAction, setModalAction] = useState<{
    type: "block" | "disconnect";
    pds: PDS;
  } | null>(null);
  const [modalConfirm, setModalConfirm] = useState<() => void>(() => {});
  const [modalCancel, setModalCancel] = useState<() => void>(() => {});

  const [editingIngestRateLimit, setEditingIngestRateLimit] =
    useState<PDS | null>(null);
  const [editingCrawlRateLimit, setEditingCrawlRateLimit] =
    useState<PDS | null>(null);

  const [adminToken, setAdminToken] = useState<string>(
    localStorage.getItem("admin_route_token") || ""
  );
  const navigate = useNavigate();

  const setAlertWithTimeout = (
    type: NotificationType,
    message: string,
    dismiss: boolean
  ) => {
    setNotification({
      message,
      alertType: type,
      autodismiss: dismiss,
    });
    setShouldShowNotification(true);
  };

  useEffect(() => {
    const token = localStorage.getItem("admin_route_token");
    if (token) {
      setAdminToken(token);
    } else {
      navigate("/login");
    }
  }, []);

  useEffect(() => {
    document.title = "BGS Admin Dashboard";
  }, []);

  const refreshPDSList = () => {
    fetch(`${BGS_HOST}/admin/pds/list`, {
      method: "GET",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${adminToken}`,
      },
    })
      .then((res) => res.json())
      .then((res: PDS[]) => {
        if ("error" in res) {
          setAlertWithTimeout(
            "failure",
            `Failed to fetch PDS list: ${res.error}`,
            true
          );
          return;
        }
        const sortedList = sortPDSList(res);
        setPDSList(sortedList);
      })
      .catch((err) => {
        setAlertWithTimeout(
          "failure",
          `Failed to fetch PDS list: ${err}`,
          true
        );
      });
  };

  const getSlurpsEnabled = () => {
    fetch(`${BGS_HOST}/admin/subs/getEnabled`, {
      method: "GET",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${adminToken}`,
      },
    })
      .then((res) => res.json())
      .then((res) => {
        if ("error" in res) {
          setAlertWithTimeout(
            "failure",
            `Failed to fetch slurp status: ${res.error}`,
            true
          );
          return;
        }
        setSlurpsEnabled(res.enabled);
      })
      .catch((err) => {
        setAlertWithTimeout(
          "failure",
          `Failed to fetch slurp status: ${err}`,
          true
        );
      });
  };

  const requestSlurpsEnabledStateChange = (state: boolean) => {
    setCanToggleSlurps(false);
    fetch(`${BGS_HOST}/admin/subs/setEnabled?enabled=${state}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${adminToken}`,
      },
    })
      .then((res) => {
        setCanToggleSlurps(true);
        if (res.status !== 200) {
          setAlertWithTimeout(
            "failure",
            `Failed to set slurp status: ${res.status}`,
            true
          );
          return;
        }
        setAlertWithTimeout(
          "success",
          `Successfully ${state ? "enabled" : "disabled"} new slurps`,
          true
        );
        setSlurpsEnabled(state);
      })
      .catch((err) => {
        setCanToggleSlurps(true);
        setAlertWithTimeout(
          "failure",
          `Failed to set slurp status: ${err}`,
          true
        );
      });
  };

  const requestCrawlHost = (host: string) => {
    fetch(`${BGS_HOST}/xrpc/com.atproto.sync.requestCrawl`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        hostname: host,
      }),
    }).then((res) => {
      if (res.status !== 200) {
        setAlertWithTimeout(
          "failure",
          `Failed to request crawl: ${res.statusText} (${res.status})`,
          true
        );
      } else {
        setAlertWithTimeout("success", "Successfully requested crawl", true);
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
          Authorization: `Bearer ${adminToken}`,
        },
      }
    ).then((res) => {
      if (res.status !== 200) {
        setAlertWithTimeout(
          "failure",
          `Failed to request ${shouldBlock ? "block" : "disconnect"}: ${
            res.statusText
          } (${res.status})`,
          true
        );
      } else {
        setAlertWithTimeout(
          "success",
          `Successfully requested ${shouldBlock ? "block" : "disconnect"}`,
          true
        );
      }
      refreshPDSList();
    });
  };

  const requestBlockHost = (host: string) => {
    fetch(`${BGS_HOST}/admin/pds/block?host=${host}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${adminToken}`,
      },
    }).then((res) => {
      if (res.status !== 200) {
        setAlertWithTimeout(
          "failure",
          `Failed to request block: ${res.statusText} (${res.status})`,
          true
        );
      } else {
        setAlertWithTimeout("success", "Successfully requested block", true);
      }
      refreshPDSList();
    });
  };
  const requestUnblockHost = (host: string) => {
    fetch(`${BGS_HOST}/admin/pds/unblock?host=${host}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${adminToken}`,
      },
    }).then((res) => {
      if (res.status !== 200) {
        setAlertWithTimeout(
          "failure",
          `Failed to request unblock: ${res.statusText} (${res.status})`,
          true
        );
      } else {
        setAlertWithTimeout("success", "Successfully requested unblock", true);
      }
      refreshPDSList();
    });
  };

  const changeIngestRateLimit = (pds: PDS, newLimit: number) => {
    fetch(
      `${BGS_HOST}/admin/pds/changeIngestRateLimit?host=${pds.Host}&limit=${newLimit}`,
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${adminToken}`,
        },
      }
    ).then((res) => {
      if (res.status !== 200) {
        setAlertWithTimeout(
          "failure",
          `Failed to change rate limit: ${res.statusText} (${res.status})`,
          true
        );
      } else {
        setAlertWithTimeout("success", "Successfully changed rate limit", true);
      }
      refreshPDSList();
    });
  };

  const changeCrawlRateLimit = (pds: PDS, newLimit: number) => {
    fetch(
      `${BGS_HOST}/admin/pds/changeCrawlRateLimit?host=${pds.Host}&limit=${newLimit}`,
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${adminToken}`,
        },
      }
    ).then((res) => {
      if (res.status !== 200) {
        setAlertWithTimeout(
          "failure",
          `Failed to change rate limit: ${res.statusText} (${res.status})`,
          true
        );
      } else {
        setAlertWithTimeout("success", "Successfully changed rate limit", true);
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
        if (shouldBlock && pds.HasActiveConnection) {
          requestDisconnectHost(pds.Host, true);
        } else if (pds.HasActiveConnection) {
          requestDisconnectHost(pds.Host, false);
        } else {
          requestBlockHost(pds.Host);
        }
        setModalAction(null);
      };
    });

    setModalCancel(() => {
      return () => {
        setModalAction(null);
      };
    });
  };

  const sortPDSList = (list: PDS[]): PDS[] => {
    const sortedPDSs: PDS[] = [...list].sort((a, b) => {
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
    return sortedPDSs;
  };

  useEffect(() => {
    if (!pdsList) {
      return;
    }
    setPDSList(sortPDSList(pdsList));
  }, [sortOrder, sortField]);

  useEffect(() => {
    refreshPDSList();
    getSlurpsEnabled();
    // Refresh stats every 10 seconds
    const interval = setInterval(() => {
      refreshPDSList();
      getSlurpsEnabled();
    }, 10 * 1000);

    return () => clearInterval(interval);
  }, [sortField, sortOrder]);

  return (
    <div className="mx-auto max-w-full">
      {shouldShowNotification ? (
        <Notification
          message={notification.message}
          alertType={notification.alertType}
          subMessage={notification.subMessage}
          autodismiss={notification.autodismiss}
          unshow={() => {
            setShouldShowNotification(false);
            setNotification({ message: "", alertType: "" });
          }}
          show={shouldShowNotification}
        ></Notification>
      ) : (
        <></>
      )}
      <div className="sm:flex sm:items-center">
        <div className="sm:flex-auto">
          <h1 className="text-2xl font-semibold leading-6 text-gray-900">
            PDS Connections
          </h1>
          <p className="mt-2 text-sm text-gray-700">
            A list of all PDS connections and their current status.
          </p>
        </div>
        <div className="inline-flex mt-5 sm:mt-0">
          <Switch.Group as="div" className="flex items-center justify-between">
            <span className="flex flex-grow flex-col mr-5">
              <Switch.Label as="span" className="text-gray-900" passive>
                {slurpsEnabled ? (
                  <ShieldCheckIcon
                    className="h-5 w-5 mr-2 mb-1 inline-block"
                    aria-hidden="true"
                  />
                ) : (
                  <ShieldExclamationIcon
                    className="h-5 w-5 mr-2 mb-1 inline-block"
                    aria-hidden="true"
                  />
                )}
                <span className="text-md font-medium leading-6">
                  New Connections {slurpsEnabled ? "Enabled" : "Disabled"}
                </span>
              </Switch.Label>
            </span>
            <Switch
              checked={slurpsEnabled}
              onChange={requestSlurpsEnabledStateChange}
              disabled={!canToggleSlurps}
              className={classNames(
                slurpsEnabled ? "bg-green-600" : "bg-red-400",
                canToggleSlurps ? "cursor-pointer" : "cursor-not-allowed",
                "relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-green-600 focus:ring-offset-2"
              )}
            >
              <span
                aria-hidden="true"
                className={classNames(
                  slurpsEnabled ? "translate-x-5" : "translate-x-0",
                  "pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                )}
              />
            </Switch>
          </Switch.Group>
        </div>
      </div>

      <div className="mt-8 flow-root">
        <div className="shadow ring-1 ring-black ring-opacity-5 sm:rounded-lg sm:rounded-b-none overflow-x-auto">
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
                      setSortField("UserCount");
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    Users
                    <span
                      className={`ml-2 flex-none rounded text-gray-400 ${
                        sortField === "UserCount"
                          ? "group-hover:bg-gray-200"
                          : "invisible group-hover:visible group-focus:visible"
                      }`}
                    >
                      {sortField === "UserCount" && sortOrder === "asc" ? (
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
                <th
                  scope="col"
                  className="px-3 py-3.5 text-right text-sm font-semibold text-gray-900 pr-6 whitespace-nowrap"
                >
                  <a href="#" className="group inline-flex">
                    Ingest Rate Limit
                  </a>
                </th>
                <th
                  scope="col"
                  className="px-3 py-3.5 text-right text-sm font-semibold text-gray-900 pr-6 whitespace-nowrap"
                >
                  <a href="#" className="group inline-flex">
                    Tokens
                  </a>
                </th>
                <th
                  scope="col"
                  className="px-3 py-3.5 text-right text-sm font-semibold text-gray-900 pr-6 whitespace-nowrap"
                >
                  <a href="#" className="group inline-flex">
                    Crawl Limit
                  </a>
                </th>
                <th
                  scope="col"
                  className="px-3 py-3.5 text-right text-sm font-semibold text-gray-900 pr-6 whitespace-nowrap"
                >
                  <a href="#" className="group inline-flex">
                    Tokens
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
                      setSortField("CreatedAt");
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    First Seen
                    <span
                      className={`ml-2 flex-none rounded text-gray-400 ${
                        sortField === "CreatedAt"
                          ? "group-hover:bg-gray-200"
                          : "invisible group-hover:visible group-focus:visible"
                      }`}
                    >
                      {sortField === "CreatedAt" && sortOrder === "asc" ? (
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
                        {pds.UserCount?.toLocaleString()}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 text-center w-8 pr-6">
                        {pds.EventsSeenSinceStartup?.toLocaleString()}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 text-center w-8 pr-6">
                        {pds.Cursor?.toLocaleString()}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 text-center w-8 pr-6">
                        <span
                          className={
                            editingIngestRateLimit?.ID === pds.ID
                              ? "hidden"
                              : ""
                          }
                        >
                          {pds.IngestRate.MaxEventsPerSecond?.toLocaleString()}
                          /sec
                        </span>
                        <input
                          type="number"
                          name={`ingest-rate-limit-${pds.ID}`}
                          id={`ingest-rate-limit-${pds.ID}`}
                          className={
                            `inline-block w-24 rounded-md border-0 py-1.5 text-gray-900 shadow-sm ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset focus:ring-indigo-600 sm:text-sm sm:leading-6` +
                            (editingIngestRateLimit?.ID === pds.ID
                              ? ""
                              : " hidden")
                          }
                          defaultValue={pds.IngestRate.MaxEventsPerSecond?.toLocaleString()}
                        />
                        <a
                          href="#"
                          onClick={() => setEditingIngestRateLimit(pds)}
                          className={editingIngestRateLimit ? "hidden" : ""}
                        >
                          <PencilSquareIcon
                            className="h-5 w-5 text-gray-500 ml-1 inline-block align-sub"
                            aria-hidden="true"
                          />
                        </a>
                        <a
                          href="#"
                          onClick={() => {
                            const newRateLimit = document.getElementById(
                              `ingest-rate-limit-${pds.ID}`
                            ) as HTMLInputElement;
                            if (newRateLimit) {
                              changeIngestRateLimit(pds, +newRateLimit.value);
                            }
                            setEditingIngestRateLimit(null);
                          }}
                          className={
                            "rounded-md p-2  ml-1 hover:text-green-600 hover:bg-green-100 focus:outline-none focus:ring-2 focus:ring-green-600 focus:ring-offset-2 focus:ring-offset-green-50" +
                            (editingIngestRateLimit?.ID === pds.ID
                              ? ""
                              : " hidden")
                          }
                        >
                          <CheckIcon
                            className="h-5 w-5 text-green-500 inline-block align-sub"
                            aria-hidden="true"
                          />
                        </a>
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 text-center w-8 pr-6">
                        {Math.abs(
                          pds.IngestRate.TokenCount || 0
                        ).toLocaleString()}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 text-center w-8 pr-6">
                        <span
                          className={
                            editingCrawlRateLimit?.ID === pds.ID ? "hidden" : ""
                          }
                        >
                          {pds.CrawlRate.MaxEventsPerSecond?.toLocaleString()}
                          /sec
                        </span>
                        <input
                          type="number"
                          name={`crawl-rate-limit-${pds.ID}`}
                          id={`crawl-rate-limit-${pds.ID}`}
                          className={
                            `inline-block w-24 rounded-md border-0 py-1.5 text-gray-900 shadow-sm ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset focus:ring-indigo-600 sm:text-sm sm:leading-6` +
                            (editingCrawlRateLimit?.ID === pds.ID
                              ? ""
                              : " hidden")
                          }
                          defaultValue={pds.CrawlRate.MaxEventsPerSecond?.toLocaleString()}
                        />
                        <a
                          href="#"
                          onClick={() => setEditingCrawlRateLimit(pds)}
                          className={editingCrawlRateLimit ? "hidden" : ""}
                        >
                          <PencilSquareIcon
                            className="h-5 w-5 text-gray-500 ml-1 inline-block align-sub"
                            aria-hidden="true"
                          />
                        </a>
                        <a
                          href="#"
                          onClick={() => {
                            const newRateLimit = document.getElementById(
                              `crawl-rate-limit-${pds.ID}`
                            ) as HTMLInputElement;
                            if (newRateLimit) {
                              changeCrawlRateLimit(pds, +newRateLimit.value);
                            }
                            setEditingCrawlRateLimit(null);
                          }}
                          className={
                            "rounded-md p-2  ml-1 hover:text-green-600 hover:bg-green-100 focus:outline-none focus:ring-2 focus:ring-green-600 focus:ring-offset-2 focus:ring-offset-green-50" +
                            (editingCrawlRateLimit?.ID === pds.ID
                              ? ""
                              : " hidden")
                          }
                        >
                          <CheckIcon
                            className="h-5 w-5 text-green-500 inline-block align-sub"
                            aria-hidden="true"
                          />
                        </a>
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 text-center w-8 pr-6">
                        {Math.abs(
                          pds.CrawlRate.TokenCount || 0
                        ).toLocaleString()}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 text-center w-8 pr-6">
                        {new Date(Date.parse(pds.CreatedAt)).toLocaleString()}
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
