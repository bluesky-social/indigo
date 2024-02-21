import { FC, useEffect, useState } from "react";
import Notification, {
  NotificationMeta,
  NotificationType,
} from "../Notification/Notification";

import { RELAY_HOST } from "../../constants";

import { useNavigate } from "react-router-dom";
import ConfirmNewPDSModal from "./ConfirmNewPDSModal";
import {
  ShieldCheckIcon,
} from "@heroicons/react/24/outline";

const NewPDS: FC<{}> = () => {
  const [pdsHost, setPDSHost] = useState<string>("");

  // Notification Management
  const [shouldShowNotification, setShouldShowNotification] =
    useState<boolean>(false);
  const [notification, setNotification] = useState<NotificationMeta>({
    message: "",
    alertType: "",
  });

  // Modal state management
  const [modalAction, setModalAction] = useState<{
    pds: string;
    type: "add" | "remove";
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

  const requestAddPDS = (pds: string) => {
    fetch(`${RELAY_HOST}/admin/pds/requestCrawl`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${adminToken}`,
      },
      body: JSON.stringify({
        hostname: pds,
      }),
    })
      .then((res) => {
        if (res.status !== 200) {
          try {
            res.json().then((data) => {
              if (data.error) {
                setAlertWithTimeout(
                  "failure",
                  `Failed to add PDS: ${data.error}`,
                  true
                );
              } else {
                setAlertWithTimeout(
                  "failure",
                  `Failed to add PDS: ${res.statusText}`,
                  true
                );
              }
            });
          }
          catch (err) {
            setAlertWithTimeout(
              "failure",
              `Failed to add PDS: ${err}`,
              true
            );
          }
        } else {
          try {
            res.json().then((data) => {
              if (data.error) {
                setAlertWithTimeout(
                  "failure",
                  `Failed to add PDS: ${data.error}`,
                  true
                );
              } else {
                setAlertWithTimeout(
                  "success",
                  `Successfully added PDS ${pds}`,
                  true
                );
              }
            });
          } catch (err) {
            setAlertWithTimeout(
              "failure",
              `Failed to add PDS: ${err}`,
              true
            );
          }
        }
      })
      .catch((err) => {
        setAlertWithTimeout("failure", `Failed to add PDS: ${err}`, true);
      });
  };

  const requestRemovePDS = (pds: string) => {
    setAlertWithTimeout(
      "failure",
      `Failed to remove PDS: ${pds} - Not implemented`,
      true
    );
  };

  const handleAddPDS = (
    pds: string,
    type: "add" | "remove"
  ) => {
    if (pds === "") {
      setAlertWithTimeout("failure", "PDS Hostname cannot be empty", true);
      return;
    }

    // Strip the protocol from the hostname
    pds = pds.replace(/^https?:\/\//, "");

    setModalAction({ pds: pds, type });

    setModalConfirm(() => {
      return () => {
        if (type === "add") requestAddPDS(pds);
        else requestRemovePDS(pds);

        setModalAction(null);
      };
    });

    setModalCancel(() => {
      return () => {
        setModalAction(null);
      };
    });
  };

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
            Add a PDS
          </h1>
          <p className="mt-2 text-sm text-gray-700">
            Add a PDS to the Relay and trigger crawling.
          </p>
        </div>
      </div>
      <div className="flex-grow mt-5">
        <div className="max-w-3xl w-full">
          <label
            htmlFor="email"
            className="block text-sm font-medium leading-6 text-gray-900"
          >
            PDS Hostname
          </label>
          <div className="mt-2 inline-flex flex-col sm:flex-row">
            <input
              type="text"
              name="pds"
              id="pds"
              className="block w-72 rounded-md border-0 py-1.5 text-gray-900 shadow-sm ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset focus:ring-indigo-600 sm:text-sm sm:leading-6"
              placeholder="hydnum.us-west.host.bsky.network"
              value={pdsHost}
              onChange={(e) => {
                setPDSHost(e.target.value);
              }}
            />
            <div className="inline-flex mt-4 sm:mt-0">
              <button
                type="button"
                onClick={() => {
                  handleAddPDS(pdsHost.trim(), "add");
                }}
                className="ml-0 sm:ml-2 inline-flex whitespace-nowrap items-center gap-x-1.5 rounded-md bg-green-600 px-2.5 py-1.5 text-sm font-semibold text-white shadow-sm hover:bg-green-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-green-600"
              >
                <ShieldCheckIcon
                  className="-ml-0.5 h-5 w-5"
                  aria-hidden="true"
                />
                Add PDS
              </button>
            </div>
          </div>
        </div>
      </div>
      {modalAction && (
        <ConfirmNewPDSModal
          action={modalAction}
          onConfirm={modalConfirm}
          onCancel={modalCancel}
        />
      )}
    </div>
  );
};

export default NewPDS;
