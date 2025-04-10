import { ChevronDownIcon, ChevronUpIcon } from "@heroicons/react/24/solid";
import { FC, useEffect, useState } from "react";
import Notification, {
  NotificationMeta,
  NotificationType,
} from "../Notification/Notification";

import { RELAY_HOST } from "../../constants";

import { useNavigate } from "react-router-dom";
import { Consumer, ConsumerKey, ConsumerResponse } from "../../models/consumer";

const Consumers: FC<{}> = () => {
  const [consumerList, setConsumerList] = useState<Consumer[] | null>(null);
  const [sortField, setSortField] = useState<ConsumerKey>("ID");
  const [sortOrder, setSortOrder] = useState<string>("asc");

  // Notification Management
  const [shouldShowNotification, setShouldShowNotification] =
    useState<boolean>(false);
  const [notification, setNotification] = useState<NotificationMeta>({
    message: "",
    alertType: "",
  });

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

  const refreshPDSList = () => {
    fetch(`${RELAY_HOST}/admin/consumers/list`, {
      method: "GET",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${adminToken}`,
      },
    })
      .then((res) => res.json())
      .then((res: ConsumerResponse[]) => {
        if ("error" in res) {
          setAlertWithTimeout(
            "failure",
            `Failed to fetch Consumer list: ${res.error}`,
            true
          );
          return;
        }
        const list: Consumer[] = res.map((consumer) => {
          return {
            RemoteAddr: consumer.remote_addr,
            UserAgent: consumer.user_agent,
            ID: consumer.id,
            EventsConsumed: consumer.events_consumed,
            ConnectedAt: new Date(Date.parse(consumer.connected_at)),
          };
        });

        const sortedList = sortConsumerList(list);
        setConsumerList(sortedList);
      })
      .catch((err) => {
        setAlertWithTimeout(
          "failure",
          `Failed to fetch Consumer list: ${err}`,
          true
        );
      });
  };

  const sortConsumerList = (list: Consumer[]): Consumer[] => {
    const sortedConsumers: Consumer[] = [...list].sort((a, b) => {
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
    return sortedConsumers;
  };

  useEffect(() => {
    if (!consumerList) {
      return;
    }
    setConsumerList(sortConsumerList(consumerList));
  }, [sortOrder, sortField]);

  useEffect(() => {
    refreshPDSList();
    // Refresh stats every 10 seconds
    const interval = setInterval(() => {
      refreshPDSList();
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
            Consumer Connections
          </h1>
          <p className="mt-2 text-sm text-gray-700">
            A list of all websocket consumers actively connected to the Relay
          </p>
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
                      setSortField("RemoteAddr");
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    Remote Address
                    <span
                      className={`ml-2 flex-none rounded text-gray-400 ${sortField === "RemoteAddr"
                        ? "group-hover:bg-gray-200"
                        : "invisible group-hover:visible group-focus:visible"
                        }`}
                    >
                      {sortField === "RemoteAddr" && sortOrder === "asc" ? (
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
                      setSortField("UserAgent");
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    User Agent
                    <span
                      className={`ml-2 flex-none rounded text-gray-400 ${sortField === "UserAgent"
                        ? "group-hover:bg-gray-200"
                        : "invisible group-hover:visible group-focus:visible"
                        }`}
                    >
                      {sortField === "UserAgent" && sortOrder === "asc" ? (
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
                      setSortField("EventsConsumed");
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    Events Consumed
                    <span
                      className={`ml-2 flex-none rounded text-gray-400 ${sortField === "EventsConsumed"
                        ? "group-hover:bg-gray-200"
                        : "invisible group-hover:visible group-focus:visible"
                        }`}
                    >
                      {sortField === "EventsConsumed" && sortOrder === "asc" ? (
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
                      setSortField("ConnectedAt");
                      setSortOrder(sortOrder === "asc" ? "desc" : "asc");
                    }}
                  >
                    Connected At
                    <span
                      className={`ml-2 flex-none rounded text-gray-400 ${sortField === "ConnectedAt"
                        ? "group-hover:bg-gray-200"
                        : "invisible group-hover:visible group-focus:visible"
                        }`}
                    >
                      {sortField === "ConnectedAt" && sortOrder === "asc" ? (
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
              {consumerList &&
                consumerList.map((consumer) => {
                  return (
                    <tr key={consumer.ID}>
                      <td className="whitespace-nowrap py-4 pl-4 pr-3 text-sm font-medium text-gray-900 sm:pl-6 text-left">
                        {consumer.ID}
                      </td>
                      <td className="whitespace-nowrap px-3 py-4 text-sm text-gray-500 text-left">
                        {consumer.RemoteAddr}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 w-8 pr-6">
                        {consumer.UserAgent}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 w-8 pr-6">
                        {consumer.EventsConsumed?.toLocaleString()}
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 text-sm text-gray-400 text-center w-8 pr-6">
                        {consumer.ConnectedAt.toLocaleString()}
                      </td>
                    </tr>
                  );
                })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};

export default Consumers;
