import { FC, useEffect, useState } from "react";
import Notification, {
  NotificationMeta,
  NotificationType,
} from "../Notification/Notification";

import { RELAY_HOST } from "../../constants";

import { useNavigate } from "react-router-dom";
import ConfirmRepoTakedownModal from "./ConfirmRepoTakedownModal";
import {
  ShieldCheckIcon,
  ShieldExclamationIcon,
} from "@heroicons/react/24/outline";

const Repos: FC<{}> = () => {
  const [repoToTakedown, setRepoToTakedown] = useState<string>("");

  // Notification Management
  const [shouldShowNotification, setShouldShowNotification] =
    useState<boolean>(false);
  const [notification, setNotification] = useState<NotificationMeta>({
    message: "",
    alertType: "",
  });

  // Modal state management
  const [modalAction, setModalAction] = useState<{
    repo: string;
    type: "takedown" | "untakedown";
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

  const requestTakedownRepo = (repo: string) => {
    fetch(`${RELAY_HOST}/admin/repo/takeDown`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${adminToken}`,
      },
      body: JSON.stringify({
        did: repo,
      }),
    })
      .then((res) => res.json())
      .then((res) => {
        if (res.error) {
          setAlertWithTimeout(
            "failure",
            `Failed to takedown repo: ${res.error}`,
            true
          );
        } else {
          setAlertWithTimeout(
            "success",
            `Successfully tookdown repo ${repo}`,
            true
          );
        }
      })
      .catch((err) => {
        setAlertWithTimeout("failure", `Failed to takedown repo: ${err}`, true);
      });
  };

  const requestUntakedownRepo = (repo: string) => {
    fetch(`${RELAY_HOST}/admin/repo/reverseTakedown`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${adminToken}`,
      },
      body: JSON.stringify({
        did: repo,
      }),
    })
      .then((res) => res.json())

      .then((res) => {
        if (res.error !== 200) {
          setAlertWithTimeout(
            "failure",
            `Failed to untakedown repo: ${res.error}`,
            true
          );
        } else {
          setAlertWithTimeout(
            "success",
            `Successfully untookdown repo ${repo}`,
            true
          );
        }
      })
      .catch((err) => {
        setAlertWithTimeout(
          "failure",
          `Failed to untakedown repo: ${err}`,
          true
        );
      });
  };

  const handleTakedownRepo = (
    repo: string,
    type: "takedown" | "untakedown"
  ) => {
    setModalAction({ repo: repo, type });

    setModalConfirm(() => {
      return () => {
        if (type === "takedown") requestTakedownRepo(repo);
        else requestUntakedownRepo(repo);

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
            Repo Takedowns
          </h1>
          <p className="mt-2 text-sm text-gray-700">
            Takedown a repo to purge it from the Relay history and reject all
            future events for it.
          </p>
        </div>
      </div>
      <div className="flex-grow mt-5">
        <div className="max-w-3xl w-full">
          <label
            htmlFor="email"
            className="block text-sm font-medium leading-6 text-gray-900"
          >
            Repo DID
          </label>
          <div className="mt-2 inline-flex flex-col sm:flex-row">
            <input
              type="text"
              name="repo"
              id="repo"
              className="block w-72 rounded-md border-0 py-1.5 text-gray-900 shadow-sm ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset focus:ring-indigo-600 sm:text-sm sm:leading-6"
              placeholder="did:plc:abadperson"
              value={repoToTakedown}
              onChange={(e) => {
                setRepoToTakedown(e.target.value);
              }}
            />
            <div className="inline-flex mt-4 sm:mt-0">
              <button
                type="button"
                onClick={() => {
                  handleTakedownRepo(repoToTakedown.trim(), "takedown");
                }}
                className="ml-0 sm:ml-2 inline-flex whitespace-nowrap items-center gap-x-1.5 rounded-md bg-red-600 px-2.5 py-1.5 text-sm font-semibold text-white shadow-sm hover:bg-red-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-red-600"
              >
                <ShieldExclamationIcon
                  className="-ml-0.5 h-5 w-5"
                  aria-hidden="true"
                />
                Takedown Repo
              </button>
              <button
                type="button"
                onClick={() => {
                  handleTakedownRepo(repoToTakedown.trim(), "untakedown");
                }}
                className="ml-2 inline-flex whitespace-nowrap items-center gap-x-1.5 rounded-md bg-green-600 px-2.5 py-1.5 text-sm font-semibold text-white shadow-sm hover:bg-green-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-green-600"
              >
                <ShieldCheckIcon
                  className="-ml-0.5 h-5 w-5"
                  aria-hidden="true"
                />
                Untakedown Repo
              </button>
            </div>
          </div>
        </div>
      </div>
      {modalAction && (
        <ConfirmRepoTakedownModal
          action={modalAction}
          onConfirm={modalConfirm}
          onCancel={modalCancel}
        />
      )}
    </div>
  );
};

export default Repos;
