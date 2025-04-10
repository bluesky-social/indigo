import {
  CheckIcon,
  ChevronDownIcon,
  ChevronUpIcon,
} from "@heroicons/react/24/solid";
import { FC, useEffect, useState } from "react";
import Notification, {
  NotificationMeta,
  NotificationType,
} from "../Notification/Notification";

import { RELAY_HOST } from "../../constants";

import { useNavigate } from "react-router-dom";
import ConfirmDomainBanModal from "./ConfirmDomainBanModal";
import { ShieldExclamationIcon } from "@heroicons/react/24/outline";

interface DomainBans {
  banned_domains: string[];
}

const Domains: FC<{}> = () => {
  const [bannedDomains, setBannedDomains] = useState<string[]>([]);
  const [sortOrder, setSortOrder] = useState<string>("asc");
  const [domainToBan, setDomainToBan] = useState<string>("");

  // Notification Management
  const [shouldShowNotification, setShouldShowNotification] =
    useState<boolean>(false);
  const [notification, setNotification] = useState<NotificationMeta>({
    message: "",
    alertType: "",
  });

  // Modal state management
  const [modalAction, setModalAction] = useState<{
    domain: string;
    type: "ban" | "unban";
  } | null>(null);
  const [modalConfirm, setModalConfirm] = useState<() => void>(() => { });
  const [modalCancel, setModalCancel] = useState<() => void>(() => { });

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

  const refreshDomainBanList = () => {
    fetch(`${RELAY_HOST}/admin/subs/listDomainBans`, {
      method: "GET",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${adminToken}`,
      },
    })
      .then((res) => res.json())
      .then((res: DomainBans) => {
        if ("error" in res) {
          setAlertWithTimeout(
            "failure",
            `Failed to fetch Domain Ban list: ${res.error}`,
            true
          );
          return;
        }
        const sortedList = sortDomainBanList(res.banned_domains);
        setBannedDomains(sortedList);
      })
      .catch((err) => {
        setAlertWithTimeout(
          "failure",
          `Failed to fetch PDS list: ${err}`,
          true
        );
      });
  };

  const requestBanDomain = (domain: string) => {
    fetch(`${RELAY_HOST}/admin/subs/banDomain`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${adminToken}`,
      },
      body: JSON.stringify({
        Domain: domain,
      }),
    })
      .then((res) => res.json())
      .then((res) => {
        if (res.error) {
          setAlertWithTimeout(
            "failure",
            `Failed to ban domain: ${res.error}`,
            true
          );
        } else {
          setAlertWithTimeout(
            "success",
            `Successfully banned domain ${domain}`,
            true
          );
        }
        refreshDomainBanList();
      })
      .catch((err) => {
        setAlertWithTimeout("failure", `Failed to ban domain: ${err}`, true);
      });
  };

  const requestUnbanDomain = (domain: string) => {
    fetch(`${RELAY_HOST}/admin/subs/unbanDomain`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${adminToken}`,
      },
      body: JSON.stringify({
        Domain: domain,
      }),
    }).then((res) => {
      if (res.status !== 200) {
        setAlertWithTimeout(
          "failure",
          `Failed to unban domain: ${res.statusText} (${res.status})`,
          true
        );
      } else {
        setAlertWithTimeout(
          "success",
          `Successfully unbanned domain ${domain}`,
          true
        );
      }
      refreshDomainBanList();
    });
  };

  const handleBanUnbaonDomain = (domain: string, type: "ban" | "unban") => {
    setModalAction({ domain, type });

    setModalConfirm(() => {
      return () => {
        if (type === "ban") requestBanDomain(domain);
        else requestUnbanDomain(domain);

        setModalAction(null);
      };
    });

    setModalCancel(() => {
      return () => {
        setModalAction(null);
      };
    });
  };

  const sortDomainBanList = (list: string[]): string[] => {
    const sortedDomains = [...list].sort();
    return sortedDomains;
  };

  useEffect(() => {
    if (!bannedDomains) {
      return;
    }
    setBannedDomains(sortDomainBanList(bannedDomains));
  }, [sortOrder]);

  useEffect(() => {
    refreshDomainBanList();
    // Refresh stats every 10 seconds
    const interval = setInterval(() => {
      refreshDomainBanList();
    }, 10 * 1000);

    return () => clearInterval(interval);
  }, [sortOrder]);

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
            Banned Domains
          </h1>
          <p className="mt-2 text-sm text-gray-700">
            A list of all currently banned domains. Any subdomains of these
            domains are also banned.
          </p>
        </div>
        <div className="flex-grow mt-5 sm:mt-0">
          <div className="max-w-3xl w-full">
            <label
              htmlFor="email"
              className="block text-sm font-medium leading-6 text-gray-900"
            >
              Domain
            </label>
            <div className="mt-2 inline-flex w-full">
              <input
                type="text"
                name="domain"
                id="domain"
                className="block w-full rounded-md border-0 py-1.5 text-gray-900 shadow-sm ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset focus:ring-indigo-600 sm:text-sm sm:leading-6"
                placeholder="noisy.pds.com"
                value={domainToBan}
                onChange={(e) => {
                  setDomainToBan(e.target.value);
                }}
              />
              <button
                type="button"
                onClick={() => {
                  handleBanUnbaonDomain(domainToBan.trim(), "ban");
                }}
                className="ml-2 inline-flex whitespace-nowrap items-center gap-x-1.5 rounded-md bg-red-600 px-2.5 py-1.5 text-sm font-semibold text-white shadow-sm hover:bg-red-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-red-600"
              >
                <ShieldExclamationIcon
                  className="-ml-0.5 h-5 w-5"
                  aria-hidden="true"
                />
                Ban Domain
              </button>
            </div>
          </div>
        </div>
      </div>

      <div className="mt-8 flow-root">
        <div className="overflow-x-auto shadow ring-1 ring-black ring-opacity-5 sm:rounded-lg sm:rounded-b-none">
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
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    Domain
                    <span className="ml-2 flex-none rounded text-gray-400 group-hover:bg-gray-200">
                      {sortOrder === "asc" ? (
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
                  className="py-3.5 pr-4 pl-3 text-right text-sm font-semibold text-gray-900 sm:pr-6"
                >
                  Unban
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-200 bg-white">
              {bannedDomains &&
                bannedDomains.map((domain) => {
                  return (
                    <tr key={domain}>
                      <td className="whitespace-nowrap py-4 pl-4 pr-3 text-sm font-medium text-gray-900 sm:pl-6 text-left">
                        {domain}
                      </td>
                      <td className="whitespace-nowrap py-4 pr-4 pl-3 text-sm font-medium text-gray-900 sm:pr-6 text-right">
                        <button
                          className="rounded-md p-1.5 hover:text-green-600 hover:bg-green-100 focus:outline-none focus:ring-2 focus:ring-green-600 focus:ring-offset-2 focus:ring-offset-green-50"
                          onClick={() => {
                            handleBanUnbaonDomain(domain, "unban");
                          }}
                        >
                          <CheckIcon className="h-5 w-5" aria-hidden="true" />
                        </button>
                      </td>
                    </tr>
                  );
                })}
            </tbody>
          </table>
        </div>
      </div>
      {modalAction && (
        <ConfirmDomainBanModal
          action={modalAction}
          onConfirm={modalConfirm}
          onCancel={modalCancel}
        />
      )}
    </div>
  );
};

export default Domains;
