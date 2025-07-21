import { useEffect } from "react";
import { useNavigate } from "react-router-dom";

export default function Logout() {
  const navigate = useNavigate();

  useEffect(() => {
    localStorage.removeItem("admin_route_token");
    navigate("/login");
  }, []);

  return <></>;
}
