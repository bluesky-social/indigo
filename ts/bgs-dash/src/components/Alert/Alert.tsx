import { FC } from "react";
import {
  CheckCircleIcon,
  ExclamationTriangleIcon,
  XMarkIcon,
} from "@heroicons/react/24/solid";

interface Alert {
  type: "error" | "success";
  message: string;
  dismissAlert: () => void;
  autoDismiss?: boolean;
}

const Alert: FC<Alert> = ({ type, message, autoDismiss, dismissAlert }) => {
  if (autoDismiss) {
    setTimeout(() => {
      dismissAlert();
    }, 5000);
  }

  return (
    <div
      className={`rounded-md p-4 ${
        type === "success" ? "bg-green-50" : "bg-red-50"
      }`}
    >
      <div className="flex">
        <div className="flex-shrink-0">
          {type === "success" ? (
            <CheckCircleIcon
              className="h-5 w-5 text-green-400"
              aria-hidden="true"
            />
          ) : (
            <ExclamationTriangleIcon
              className="h-5 w-5 text-red-400"
              aria-hidden="true"
            />
          )}
        </div>
        <div className="ml-3">
          <p
            className={`text-sm font-medium ${
              type === "success" ? "text-green-800" : "text-red-800"
            }`}
          >
            {message}
          </p>
        </div>
        <div className="ml-auto pl-3">
          <div className="-mx-1.5 -my-1.5">
            <button
              type="button"
              className={`inline-flex rounded-md p-1.5 ${
                type === "success"
                  ? "text-green-500 bg-green-50 hover:bg-green-100"
                  : "text-red-500 bg-red-50 hover:bg-red-100"
              } focus:outline-none focus:ring-2 focus:ring-offset-2 ${
                type === "success"
                  ? "focus:ring-green-600 focus:ring-offset-green-50"
                  : "focus:ring-red-600 focus:ring-offset-red-50"
              }`}
              onClick={dismissAlert}
            >
              <span className="sr-only">Dismiss</span>
              <XMarkIcon className="h-5 w-5" aria-hidden="true" />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default Alert;
