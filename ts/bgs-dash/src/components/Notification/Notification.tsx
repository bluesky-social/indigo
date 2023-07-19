import { Fragment, useEffect } from "react";
import { Transition } from "@headlessui/react";
import {
  ArrowUpCircleIcon,
  ArrowDownCircleIcon,
  CheckCircleIcon,
  XCircleIcon,
} from "@heroicons/react/24/outline";
import { XMarkIcon } from "@heroicons/react/24/solid";

interface NotificationProps {
  alertType: string;
  message: string;
  subMessage?: string;
  component?: JSX.Element;
  show: boolean;
  unshow: Function;
  autodismiss?: boolean;
}

export interface NotificationMeta {
  message: string;
  subMessage?: string;
  alertType: string;
  autodismiss?: boolean;
}

export type NotificationType = "success" | "failure" | "loading" | "uploading";

export default function Notification(props: NotificationProps) {
  useEffect(() => {
    if (props.autodismiss) {
      setTimeout(() => {
        props.unshow();
      }, 3500);
    }
  }, []);

  return (
    <>
      {/* Global notification live region, render this permanently at the end of the document */}
      <div
        aria-live="assertive"
        className="fixed inset-0 flex items-end px-4 py-6 pointer-events-none sm:p-6 sm:items-start z-20"
      >
        <div className="w-full flex flex-col items-center space-y-4 sm:items-end mr-10">
          {/* Notification panel, dynamically insert this into the live region when it needs to be displayed */}
          <Transition
            show={props.show}
            as={Fragment}
            enter="transform ease-out duration-300 transition"
            enterFrom="translate-y-2 opacity-0 sm:translate-y-0 sm:translate-x-2"
            enterTo="translate-y-0 opacity-100 sm:translate-x-0"
            leave="transition ease-in duration-100"
            leaveFrom="opacity-100"
            leaveTo="opacity-0"
          >
            <div className="max-w-md w-full bg-white shadow-lg rounded-lg pointer-events-auto ring-1 ring-black ring-opacity-5 overflow-hidden">
              <div className="p-4">
                <div className="flex items-start">
                  <div className="flex-shrink-0">
                    {props.alertType === "success" ? (
                      <CheckCircleIcon
                        className="h-6 w-6 text-green-400"
                        aria-hidden="true"
                      />
                    ) : (
                      <></>
                    )}
                    {props.alertType === "failure" ? (
                      <XCircleIcon
                        className="h-6 w-6 text-red-400"
                        aria-hidden="true"
                      />
                    ) : (
                      <></>
                    )}
                    {props.alertType === "loading" ? (
                      <ArrowDownCircleIcon
                        className="h-6 w-6 text-blue-400"
                        aria-hidden="true"
                      />
                    ) : (
                      <></>
                    )}
                    {props.alertType === "uploading" ? (
                      <ArrowUpCircleIcon
                        className="h-6 w-6 text-blue-400"
                        aria-hidden="true"
                      />
                    ) : (
                      <></>
                    )}
                  </div>
                  <div className="ml-3 w-0 flex-1 pt-0.5">
                    <p className="text-sm font-medium text-gray-900">
                      {props.message}
                    </p>
                    {props.subMessage ? (
                      <p className="mt-1 text-sm text-gray-500">
                        {props.subMessage}
                      </p>
                    ) : (
                      <></>
                    )}
                    {props.component ? props.component : <></>}
                  </div>
                  <div className="ml-4 flex-shrink-0 flex">
                    <button
                      className="bg-white rounded-md inline-flex text-gray-400 hover:text-gray-500 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500"
                      onClick={(e) => {
                        props.unshow();
                      }}
                    >
                      <span className="sr-only">Close</span>
                      <XMarkIcon className="h-5 w-5" aria-hidden="true" />
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </Transition>
        </div>
      </div>
    </>
  );
}
