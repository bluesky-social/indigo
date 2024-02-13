import React, { useState } from "react";
import { useNavigate } from "react-router-dom";
import { RELAY_HOST } from "../../constants";
import Notification, { NotificationMeta } from "../Notification/Notification";

export default function Login() {
  const [token, setToken] = useState("");
  const navigate = useNavigate();

  // Notification Management
  const [shouldShowNotification, setShouldShowNotification] =
    useState<boolean>(false);
  const [notification, setNotification] = useState<NotificationMeta>({
    message: "",
    alertType: "",
  });

  const handleSaveToken = (e: React.FormEvent) => {
    e.preventDefault();

    if (token) {
      // Try to make a request to the Admin API to verify the token
      fetch(`${RELAY_HOST}/admin/pds/list`, {
        method: "GET",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${token}`,
        },
      })
        .then((res) => {
          if (res.status !== 200) {
            setNotification({
              message: `Failed to validate Admin Token: Status ${res.status}`,
              alertType: "failure",
              autodismiss: true,
            });
            setShouldShowNotification(true);
            return;
          }
          localStorage.setItem("admin_route_token", token);
          setToken("");
          navigate("/");
        })
        .catch((err) => {
          setNotification({
            message: `Failed to validate Admin Token: ${err}`,
            alertType: "failure",
            autodismiss: true,
          });
          setShouldShowNotification(true);
        });
    }
  };

  return (
    <>
      <div className="flex min-h-full flex-1 flex-col justify-center px-6 py-12 lg:px-8">
        <div className="sm:mx-auto sm:w-full sm:max-w-sm">
          <div className="my-4">
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
          </div>
          <h2 className=" text-center text-2xl font-bold leading-9 tracking-tight text-gray-900">
            Login to the Relay Admin Dashboard
          </h2>
        </div>

        <div className="mt-10 sm:mx-auto sm:w-full sm:max-w-sm">
          <form className="space-y-6" onSubmit={handleSaveToken}>
            <div>
              <label
                htmlFor="token"
                className="block text-sm font-medium leading-6 text-gray-900"
              >
                Access Token
              </label>
              <div className="mt-2">
                <input
                  id="token"
                  name="token"
                  type="password"
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  required
                  className="block w-full rounded-md border-0 py-1.5 text-gray-900 shadow-sm ring-1 ring-inset ring-gray-300 placeholder:text-gray-400 focus:ring-2 focus:ring-inset focus:ring-indigo-600 sm:text-sm sm:leading-6"
                />
              </div>
            </div>

            <div>
              <button
                type="submit"
                className="flex w-full justify-center rounded-md bg-indigo-600 px-3 py-1.5 text-sm font-semibold leading-6 text-white shadow-sm hover:bg-indigo-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-indigo-600"
              >
                Set Token
              </button>
            </div>
          </form>
        </div>
      </div>
    </>
  );
}
