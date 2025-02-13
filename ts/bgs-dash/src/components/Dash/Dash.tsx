import {
  ChevronDoubleLeftIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  MagnifyingGlassIcon,
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

import { RELAY_HOST } from "../../constants";
import { PDS, PDSKey } from "../../models/pds";

import { Switch } from "@headlessui/react";
import { useNavigate } from "react-router-dom";
import ConfirmModal from "./ConfirmModal";

function classNames(...classes: string[]) {
  return classes.filter(Boolean).join(" ");
}

const Dash: FC<{}> = () => {
  const [pdsList, setPDSList] = useState<PDS[] | null>(null);
  const [fullPDSList, setFullPDSList] = useState<PDS[] | null>(null);
  const [sortField, setSortField] = useState<PDSKey>("ID");
  const [sortOrder, setSortOrder] = useState<string>("asc");
  const [pageNum, setPageNum] = useState<number>(1);
  const pageSize = 30;

  // Slurp Toggle Management
  const [slurpsEnabled, setSlurpsEnabled] = useState<boolean>(true);
  const [canToggleSlurps, setCanToggleSlurps] = useState<boolean>(true);
  const [newPDSLimit, setNewPDSLimit] = useState<number>(0);
  const [canSetNewPDSLimit, setCanSetNewPDSLimit] = useState<boolean>(true);

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
  const [modalConfirm, setModalConfirm] = useState<() => void>(() => { });
  const [modalCancel, setModalCancel] = useState<() => void>(() => { });

  const [editingPerSecondRateLimit, setEditingPerSecondRateLimimt] =
    useState<PDS | null>(null);
  const [editingPerHourRateLimit, setEditingPerHourRateLimit] =
    useState<PDS | null>(null);
  const [editingPerDayRateLimit, setEditingPerDayRateLimit] =
    useState<PDS | null>(null);
  const [editingCrawlRateLimit, setEditingCrawlRateLimit] =
    useState<PDS | null>(null);
  const [editingRepoLimit, setEditingRepoLimit] =
    useState<PDS | null>(null);

  const [searchTerm, setSearchTerm] = useState<string | undefined>(undefined);

  const filterPDSList = (list: PDS[]): PDS[] => {
    // Filter the hostnames based on the search term
    if (searchTerm) {
      // Support RegEx search
      try {
        const regex = new RegExp(searchTerm, "i");
        list = list.filter((pds) => {
          return regex.test(pds.Host);
        });
      } catch (e) {
        // If the regex is invalid, just do a normal search
        list = list.filter((pds) => {
          return pds.Host.toLowerCase().includes(searchTerm.toLowerCase());
        });
      }
    }

    return list;
  };

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
    document.title = "Relay Admin Dashboard";
  }, []);

  const refreshPDSList = () => {
    fetch(`${RELAY_HOST}/admin/pds/list`, {
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
        setFullPDSList(res);
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
    fetch(`${RELAY_HOST}/admin/subs/getEnabled`, {
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
    fetch(`${RELAY_HOST}/admin/subs/setEnabled?enabled=${state}`, {
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

  const getNewPDSRateLimit = () => {
    fetch(`${RELAY_HOST}/admin/subs/perDayLimit`, {
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
            `Failed to fetch New PDS rate limit: ${res.error}`,
            true
          );
          return;
        }
        setNewPDSLimit(res.limit);
      })
      .catch((err) => {
        setAlertWithTimeout(
          "failure",
          `Failed to fetch New PDS rate limit: ${err}`,
          true
        );
      });
  }

  const setNewPDSRateLimit = (limit: number) => {
    setCanSetNewPDSLimit(false);
    fetch(`${RELAY_HOST}/admin/subs/setPerDayLimit?limit=${limit}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${adminToken}`,
      },

    })
      .then((res) => {
        setCanSetNewPDSLimit(true);
        if (res.status !== 200) {
          setAlertWithTimeout(
            "failure",
            `Failed to set New PDS rate limit: ${res.status}`,
            true
          );
          return;
        }
        setAlertWithTimeout(
          "success",
          `Successfully set New PDS rate limit to ${limit} / day`,
          true
        );
        setNewPDSLimit(limit);
      })
      .catch((err) => {
        setCanSetNewPDSLimit(true);
        setAlertWithTimeout(
          "failure",
          `Failed to set New PDS rate limit: ${err}`,
          true
        );
      });
  }

  const requestCrawlHost = (host: string) => {
    fetch(`${RELAY_HOST}/xrpc/com.atproto.sync.requestCrawl`, {
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
      `${RELAY_HOST}/admin/subs/killUpstream?host=${host}&block=${shouldBlock}`,
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
          `Failed to request ${shouldBlock ? "block" : "disconnect"}: ${res.statusText
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
    fetch(`${RELAY_HOST}/admin/pds/block?host=${host}`, {
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
    fetch(`${RELAY_HOST}/admin/pds/unblock?host=${host}`, {
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

  const updateRateLimits = (pds: PDS) => {
    fetch(
      `${RELAY_HOST}/admin/pds/changeLimits`,
      {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${adminToken}`,
        },
        body: JSON.stringify({
          host: pds.Host,
          per_second: pds.PerSecondEventRate.Max,
          per_hour: pds.PerHourEventRate.Max,
          per_day: pds.PerDayEventRate.Max,
          crawl_rate: pds.CrawlRate.Max,
          repo_limit: pds.RepoLimit,
        }),
      }
    ).then((res) => {
      if (res.status !== 200) {
        setAlertWithTimeout(
          "failure",
          `Failed to change rate limit: ${res.statusText} (${res.status})`,
          true
        );
      } else {
        setAlertWithTimeout("success", "Successfully changed rate limits", true);
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
    if (!fullPDSList) {
      return;
    }
    setPDSList(sortPDSList(filterPDSList(fullPDSList!)));
  }, [sortOrder, sortField, searchTerm, fullPDSList]);

  useEffect(() => {
    refreshPDSList();
    getSlurpsEnabled();
    getNewPDSRateLimit();
    // Refresh stats every 60 seconds
    const interval = setInterval(() => {
      refreshPDSList();
      getSlurpsEnabled();
      getNewPDSRateLimit();
    }, 60 * 1000);

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
      <div></div>
      <div className="sm:flex sm:items-center">
        <div className="sm:flex-auto">
          <h1 className="text-2xl font-semibold leading-6 text-gray-900">
            PDS Connections
          </h1>
          <p className="mt-2 text-sm text-gray-700">
            A list of all PDS connections and their current status.
          </p>
        </div>
        <div className="flex flex-col mt-5">
          <div className="inline-flex mt-5 sm:mt-0 flex-col">
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
          <div className="ml-4">
            <div className="mt-2 flex rounded-md shadow-sm">
              <div className="relative flex flex-grow items-stretch focus-within:z-10">
                <input
                  type="number"
                  id="new-pds-rate-limit"
                  name="new-pds-rate-limit"
                  // Hides the up/down arrows on number inputs
                  className="[appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none block w-full rounded-none rounded-l-md border-0 py-1.5 text-gray-900 ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset focus:ring-indigo-600 sm:text-sm sm:leading-6"
                  value={newPDSLimit}
                  aria-describedby="rate-limit"
                  onChange={(e) => {
                    setNewPDSLimit(parseInt(e.target.value));
                  }}
                />
                <div className="pointer-events-none absolute inset-y-0 right-0 flex items-center pr-3">
                  <span className="text-gray-500 sm:text-sm" id="price-currency">
                    PDS / Day
                  </span>
                </div>
              </div>
              <button
                type="button"
                className="relative -ml-px inline-flex items-center gap-x-1.5 rounded-r-md px-3 py-2 text-sm font-semibold text-gray-900 ring-1 ring-inset ring-gray-300 hover:bg-gray-50"
                disabled={!canSetNewPDSLimit}
                onClick={() => {
                  setNewPDSRateLimit(newPDSLimit);
                }}
              >
                Update
              </button>
            </div>
          </div>
        </div>

      </div>
      <div className="flex flex-1 items-center justify-center py-2 lg:justify-start">
        <div className="w-full max-w-lg lg:max-w-xs">
          <label htmlFor="search" className="sr-only">
            Search
          </label>
          <div className="relative">
            <div className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3">
              <MagnifyingGlassIcon className="h-5 w-5 text-gray-400" aria-hidden="true" />
            </div>
            <input
              id="search"
              name="search"
              className="block w-full rounded-md border-0 bg-white py-1.5 pl-10 pr-3 text-gray-900 ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset focus:ring-indigo-600 sm:text-sm sm:leading-6"
              placeholder="Search"
              type="search"
              onChange={(e) => {
                setSearchTerm(e.target.value);
                setPageNum(1);
              }}
              value={searchTerm || ""}
            />
          </div>
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
                      className={`ml-2 flex-none rounded text-gray-400 ${sortField === "ID"
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
                      className={`ml-2 flex-none rounded text-gray-400 ${sortField === "Host"
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
                      className={`ml-2 flex-none rounded text-gray-400 ${sortField === "HasActiveConnection"
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
                      className={`ml-2 flex-none rounded text-gray-400 ${sortField === "Blocked"
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
                      setSortField("RepoCount");
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    Users
                    <span
                      className={`ml-2 flex-none rounded text-gray-400 ${sortField === "RepoCount"
                        ? "group-hover:bg-gray-200"
                        : "invisible group-hover:visible group-focus:visible"
                        }`}
                    >
                      {sortField === "RepoCount" && sortOrder === "asc" ? (
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
                      className={`ml-2 flex-none rounded text-gray-400 ${sortField === "EventsSeenSinceStartup"
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
                      className={`ml-2 flex-none rounded text-gray-400 ${sortField === "Cursor"
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
                    Events Per Second Limit
                  </a>
                </th>
                <th
                  scope="col"
                  className="px-3 py-3.5 text-right text-sm font-semibold text-gray-900 pr-6 whitespace-nowrap"
                >
                  <a href="#" className="group inline-flex">
                    Per Hour Limit
                  </a>
                </th>
                <th
                  scope="col"
                  className="px-3 py-3.5 text-right text-sm font-semibold text-gray-900 pr-6 whitespace-nowrap"
                >
                  <a href="#" className="group inline-flex">
                    Per Day Limit
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
                    Repo Limit
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
                      className={`ml-2 flex-none rounded text-gray-400 ${sortField === "CreatedAt"
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
                pdsList.map((pds, idx) => {
                  if (idx < (pageNum - 1) * pageSize || idx >= pageNum * pageSize) {
                    return null;
                  }
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
                        {pds.RepoCount?.toLocaleString()}
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
                            editingPerSecondRateLimit?.ID === pds.ID
                              ? "hidden"
                              : ""
                          }
                        >
                          {pds.PerSecondEventRate.Max?.toLocaleString()}
                          /sec
                        </span>
                        <input
                          type="number"
                          name={`per-second-rate-limit-${pds.ID}`}
                          id={`per-second-rate-limit-${pds.ID}`}
                          className={
                            `inline-block w-24 rounded-md border-0 py-1.5 text-gray-900 shadow-sm ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset focus:ring-indigo-600 sm:text-sm sm:leading-6` +
                            (editingPerSecondRateLimit?.ID === pds.ID
                              ? ""
                              : " hidden")
                          }
                          defaultValue={pds.PerSecondEventRate.Max?.toLocaleString()}
                        />
                        <a
                          href="#"
                          onClick={() => setEditingPerSecondRateLimimt(pds)}
                          className={editingPerSecondRateLimit ? "hidden" : ""}
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
                              `per-second-rate-limit-${pds.ID}`
                            ) as HTMLInputElement;
                            if (newRateLimit) {
                              pds.PerSecondEventRate.Max = +newRateLimit.value;
                              updateRateLimits(pds);
                            }
                            setEditingPerSecondRateLimimt(null);
                          }}
                          className={
                            "rounded-md p-2  ml-1 hover:text-green-600 hover:bg-green-100 focus:outline-none focus:ring-2 focus:ring-green-600 focus:ring-offset-2 focus:ring-offset-green-50" +
                            (editingPerSecondRateLimit?.ID === pds.ID
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
                        <span
                          className={
                            editingPerHourRateLimit?.ID === pds.ID
                              ? "hidden"
                              : ""
                          }
                        >
                          {pds.PerHourEventRate.Max?.toLocaleString()}
                          /hour
                        </span>
                        <input
                          type="number"
                          name={`per-hour-rate-limit-${pds.ID}`}
                          id={`per-hour-rate-limit-${pds.ID}`}
                          className={
                            `inline-block w-24 rounded-md border-0 py-1.5 text-gray-900 shadow-sm ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset focus:ring-indigo-600 sm:text-sm sm:leading-6` +
                            (editingPerHourRateLimit?.ID === pds.ID
                              ? ""
                              : " hidden")
                          }
                          defaultValue={pds.PerHourEventRate.Max?.toLocaleString()}
                        />
                        <a
                          href="#"
                          onClick={() => setEditingPerHourRateLimit(pds)}
                          className={editingPerHourRateLimit ? "hidden" : ""}
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
                              `per-hour-rate-limit-${pds.ID}`
                            ) as HTMLInputElement;
                            if (newRateLimit) {
                              pds.PerHourEventRate.Max = +newRateLimit.value;
                              updateRateLimits(pds);
                            }
                            setEditingPerHourRateLimit(null);
                          }}
                          className={
                            "rounded-md p-2  ml-1 hover:text-green-600 hover:bg-green-100 focus:outline-none focus:ring-2 focus:ring-green-600 focus:ring-offset-2 focus:ring-offset-green-50" +
                            (editingPerHourRateLimit?.ID === pds.ID
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
                        <span
                          className={
                            editingPerDayRateLimit?.ID === pds.ID
                              ? "hidden"
                              : ""
                          }
                        >
                          {pds.PerDayEventRate.Max?.toLocaleString()}
                          /day
                        </span>
                        <input
                          type="number"
                          name={`per-day-limit-${pds.ID}`}
                          id={`per-day-rate-limit-${pds.ID}`}
                          className={
                            `inline-block w-24 rounded-md border-0 py-1.5 text-gray-900 shadow-sm ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset focus:ring-indigo-600 sm:text-sm sm:leading-6` +
                            (editingPerDayRateLimit?.ID === pds.ID
                              ? ""
                              : " hidden")
                          }
                          defaultValue={pds.PerDayEventRate.Max?.toLocaleString()}
                        />
                        <a
                          href="#"
                          onClick={() => setEditingPerDayRateLimit(pds)}
                          className={editingPerDayRateLimit ? "hidden" : ""}
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
                              `per-day-rate-limit-${pds.ID}`
                            ) as HTMLInputElement;
                            if (newRateLimit) {
                              pds.PerDayEventRate.Max = +newRateLimit.value;
                              updateRateLimits(pds);
                            }
                            setEditingPerDayRateLimit(null);
                          }}
                          className={
                            "rounded-md p-2  ml-1 hover:text-green-600 hover:bg-green-100 focus:outline-none focus:ring-2 focus:ring-green-600 focus:ring-offset-2 focus:ring-offset-green-50" +
                            (editingPerDayRateLimit?.ID === pds.ID
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
                        <span
                          className={
                            editingCrawlRateLimit?.ID === pds.ID
                              ? "hidden"
                              : ""
                          }
                        >
                          {pds.CrawlRate.Max?.toLocaleString()}
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
                          defaultValue={pds.CrawlRate.Max?.toLocaleString()}
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
                              pds.CrawlRate.Max = +newRateLimit.value;
                              updateRateLimits(pds);
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
                        <span
                          className={
                            editingRepoLimit?.ID === pds.ID
                              ? "hidden"
                              : ""
                          }
                        >
                          {pds.RepoLimit?.toLocaleString()}
                        </span>
                        <input
                          type="number"
                          name={`repo-limit-${pds.ID}`}
                          id={`repo-limit-${pds.ID}`}
                          className={
                            `inline-block w-24 rounded-md border-0 py-1.5 text-gray-900 shadow-sm ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset focus:ring-indigo-600 sm:text-sm sm:leading-6` +
                            (editingRepoLimit?.ID === pds.ID
                              ? ""
                              : " hidden")
                          }
                          defaultValue={pds.RepoLimit?.toLocaleString()}
                        />
                        <a
                          href="#"
                          onClick={() => setEditingRepoLimit(pds)}
                          className={editingRepoLimit ? "hidden" : ""}
                        >
                          <PencilSquareIcon
                            className="h-5 w-5 text-gray-500 ml-1 inline-block align-sub"
                            aria-hidden="true"
                          />
                        </a>
                        <a
                          href="#"
                          onClick={() => {
                            const newLimit = document.getElementById(
                              `repo-limit-${pds.ID}`
                            ) as HTMLInputElement;
                            if (newLimit) {
                              pds.RepoLimit = +newLimit.value;
                              updateRateLimits(pds);
                            }
                            setEditingRepoLimit(null);
                          }}
                          className={
                            "rounded-md p-2  ml-1 hover:text-green-600 hover:bg-green-100 focus:outline-none focus:ring-2 focus:ring-green-600 focus:ring-offset-2 focus:ring-offset-green-50" +
                            (editingRepoLimit?.ID === pds.ID
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
                        {new Date(Date.parse(pds.CreatedAt)).toLocaleString()}
                      </td>
                    </tr>
                  );
                })}
            </tbody>
          </table>
        </div>
        {pdsList && pdsList.length > pageSize && (
          <div className="mt-4 flex-1 flex justify-between sm:justify-end">
            <div className="flex-1 flex justify-between sm:hidden">
              <button
                onClick={() => {
                  if (pageNum > 1) {
                    setPageNum(pageNum - 1);
                  }
                }}
                disabled={pageNum <= 1}
                className="relative inline-flex items-center px-4 py-2 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-md shadow-sm cursor-pointer"
              >
                Previous
              </button>
              <button
                onClick={() => {
                  if (pageNum < Math.ceil(pdsList.length / pageSize)) {
                    setPageNum(pageNum + 1);
                  }
                }}
                disabled={pageNum >= Math.ceil(pdsList.length / pageSize)}
                className="relative inline-flex items-center px-4 py-2 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-md shadow-sm cursor-pointer"
              >
                Next
              </button>
            </div>
            <div className="hidden sm:flex-1 sm:flex sm:items-center sm:justify-between">
              <div>
                <p className="text-sm text-gray-700">
                  Showing
                  <span className="font-medium"> {1 + (pageNum - 1) * pageSize} </span>
                  to
                  <span className="font-medium"> {Math.min(pageNum * pageSize, pdsList.length)} </span>
                  of
                  <span className="font-medium"> {pdsList.length} </span>
                  results
                </p>
              </div>
              <div>
                <nav className="relative z-0 inline-flex rounded-md shadow-sm -space-x-px" aria-label="Pagination">
                  <button
                    onClick={() => setPageNum(1)}
                    disabled={pageNum <= 1}
                    className="relative inline-flex items-center px-2 py-2 text-sm font-medium text-gray-500 bg-white border border-gray-300 cursor-pointer"
                  >
                    <span className="sr-only">First</span>
                    <ChevronDoubleLeftIcon className="h-5 w-5" aria-hidden="true" />
                  </button>
                  <button
                    onClick={() => {
                      if (pageNum > 1) {
                        setPageNum(pageNum - 1);
                      }
                    }}
                    disabled={pageNum <= 1}
                    className="relative inline-flex items-center px-2 py-2 text-sm font-medium text-gray-500 bg-white border border-gray-300 cursor-pointer"
                  >
                    <span className="sr-only">Previous</span>
                    <ChevronLeftIcon className="h-5 w-5" aria-hidden="true" />
                  </button>
                  {Array.from({ length: Math.ceil(pdsList.length / pageSize) }, (_, i) => i + 1).map((page) => (
                    // Skip buttons more than 5 pages away from the current page
                    Math.abs(page - pageNum) > 5 ? null : (
                      <button
                        key={page}
                        onClick={() => setPageNum(page)}
                        className={classNames(
                          page === pageNum
                            ? "z-10 bg-indigo-50 border-indigo-500 text-indigo-600"
                            : "bg-white border-gray-300 text-gray-500",
                          "relative inline-flex items-center px-4 py-2 text-sm font-medium border cursor-pointer"
                        )}
                      >
                        {page}
                      </button>
                    )
                  ))}
                  <button
                    onClick={() => {
                      if (pageNum < Math.ceil(pdsList.length / pageSize)) {
                        setPageNum(pageNum + 1);
                      }
                    }}
                    disabled={pageNum >= Math.ceil(pdsList.length / pageSize)}
                    className="relative inline-flex items-center px-2 py-2 text-sm font-medium text-gray-500 bg-white border border-gray-300 cursor-pointer"
                  >
                    <span className="sr-only">Next</span>
                    <ChevronRightIcon className="h-5 w-5" aria-hidden="true" />
                  </button>
                </nav>
              </div>
            </div>
          </div>
        )}
      </div>
      {
        modalAction && (
          <ConfirmModal
            action={modalAction}
            onConfirm={modalConfirm}
            onCancel={modalCancel}
          />
        )
      }
    </div >
  );
};

export default Dash;
